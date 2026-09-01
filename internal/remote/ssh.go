// Package remote forwards pism subcommands to a pism binary on another host
// over the system ssh client. It never handles credentials itself, relying on
// the user's existing ssh config / keys / agent.
//
// SSH config resolution: the system ssh already reads ~/.ssh/config. On top of
// that, pism will prefer a project-local config when present (or one given via
// --ssh-config / $PISM_SSH_CONFIG), passing it to ssh/scp with -F.
package remote

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// Options controls how a command is forwarded.
type Options struct {
	Host       string // ssh target (user@host, or a Host alias)
	RemoteBin  string // pism path on the remote (default "pism")
	TTY        bool   // allocate a pty (ssh -t) — required for attach
	ConfigFile string // explicit ssh config file (-F); "" = ssh defaults
}

// localConfigCandidates are project-local ssh config paths, checked in order.
var localConfigCandidates = []string{
	"ssh_config",
	".ssh_config",
	filepath.Join(".ssh", "config"),
	filepath.Join(".pism", "ssh_config"),
}

// ResolveConfig picks which ssh config file to use:
//  1. an explicit path (from --ssh-config),
//  2. $PISM_SSH_CONFIG,
//  3. the first existing project-local candidate in the current directory.
//
// Returns "" when none apply, in which case ssh uses its own defaults
// (~/.ssh/config and /etc/ssh/ssh_config).
func ResolveConfig(explicit string) string {
	if explicit != "" {
		if abs, err := filepath.Abs(explicit); err == nil {
			return abs
		}
		return explicit
	}
	if e := os.Getenv("PISM_SSH_CONFIG"); e != "" {
		return e
	}
	for _, c := range localConfigCandidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			if abs, err := filepath.Abs(c); err == nil {
				return abs
			}
			return c
		}
	}
	return ""
}

// configArgs returns the -F flag pair when a config file is set.
func configArgs(cfg string) []string {
	if cfg == "" {
		return nil
	}
	return []string{"-F", cfg}
}

// Forward runs `ssh [-F cfg] [-t] host <remoteBin> <args...>` inheriting stdio
// and returns the remote exit code.
func Forward(o Options, args []string) (int, error) {
	bin := o.RemoteBin
	if bin == "" {
		bin = "pism"
	}
	sshArgs := configArgs(o.ConfigFile)
	if o.TTY {
		sshArgs = append(sshArgs, "-t")
	}
	sshArgs = append(sshArgs, o.Host, bin)
	sshArgs = append(sshArgs, args...)

	cmd := exec.Command("ssh", sshArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), nil
	}
	return 1, err
}

// Push copies a matching-architecture pism binary to the remote host.
// It detects the remote OS/arch via `ssh host uname -sm` (POSIX hosts) and
// scp's the corresponding binary from distDir into ~/.local/bin (or a given
// destination), making it executable. The same ssh config file is used for
// all ssh/scp invocations.
func Push(host, distDir, dest, cfg string) error {
	ssh := func(a ...string) *exec.Cmd {
		return exec.Command("ssh", append(configArgs(cfg), a...)...)
	}
	out, err := ssh(host, "uname -sm").Output()
	if err != nil {
		return fmt.Errorf("detect remote platform (is it POSIX? for Windows copy pism.exe manually): %w", err)
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) < 2 {
		return fmt.Errorf("unexpected uname output %q", string(out))
	}
	goos := map[string]string{"Linux": "linux", "Darwin": "darwin"}[fields[0]]
	goarch := map[string]string{"x86_64": "amd64", "aarch64": "arm64", "arm64": "arm64"}[fields[1]]
	if goos == "" || goarch == "" {
		return fmt.Errorf("unsupported remote platform %q %q", fields[0], fields[1])
	}
	local := fmt.Sprintf("%s/pism-%s-%s", strings.TrimRight(distDir, "/"), goos, goarch)
	if _, err := os.Stat(local); err != nil {
		return fmt.Errorf("no prebuilt binary %s (run: pism build-all)", local)
	}
	if dest == "" {
		dest = ".local/bin/pism"
	}
	// Ensure dest dir exists, then scp, then chmod.
	destDir := dest[:strings.LastIndex(dest, "/")]
	if destDir != "" {
		if _, err := ssh(host, "mkdir -p "+shellQuote(destDir)).CombinedOutput(); err != nil {
			return fmt.Errorf("mkdir remote dir: %w", err)
		}
	}
	scp := exec.Command("scp", append(configArgs(cfg), "-q", local, host+":"+dest)...)
	scp.Stderr = os.Stderr
	if err := scp.Run(); err != nil {
		return fmt.Errorf("scp: %w", err)
	}
	if _, err := ssh(host, "chmod +x "+shellQuote(dest)).CombinedOutput(); err != nil {
		return fmt.Errorf("chmod remote: %w", err)
	}
	fmt.Fprintf(os.Stderr, "pushed %s -> %s:%s\n", local, host, dest)
	return nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// ListConfigHosts parses concrete `Host` aliases out of an ssh config file.
// Pattern entries (containing '*', '?' or the '!' negation) are skipped since
// they aren't connectable targets. When cfg is "" it falls back to the user's
// ~/.ssh/config. Order is preserved and duplicates removed.
func ListConfigHosts(cfg string) ([]string, error) {
	pathToCfg := cfg
	if pathToCfg == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("locate home dir: %w", err)
		}
		pathToCfg = filepath.Join(home, ".ssh", "config")
	}
	f, err := os.Open(pathToCfg)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var hosts []string
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, rest := splitConfigLine(line)
		if !strings.EqualFold(key, "Host") {
			continue
		}
		for _, tok := range strings.Fields(rest) {
			if strings.ContainsAny(tok, "*?!") {
				continue // pattern, not a concrete host
			}
			if !seen[tok] {
				seen[tok] = true
				hosts = append(hosts, tok)
			}
		}
	}
	return hosts, sc.Err()
}

// splitConfigLine splits an ssh_config line into keyword and the rest of the
// value. ssh accepts either whitespace or an '=' (optionally surrounded by
// whitespace) between a keyword and its arguments.
func splitConfigLine(line string) (key, rest string) {
	i := strings.IndexAny(line, " \t=")
	if i < 0 {
		return line, ""
	}
	key = line[:i]
	rest = strings.TrimLeft(line[i:], " \t=")
	return key, rest
}

// MatchesAny reports whether host matches any of the glob patterns (path.Match
// semantics: '*' and '?'). A plain host name is an exact match. An empty
// pattern list is never a match.
func MatchesAny(host string, patterns []string) bool {
	for _, p := range patterns {
		if p == host {
			return true
		}
		if ok, err := path.Match(p, host); err == nil && ok {
			return true
		}
	}
	return false
}

// RunAllOptions controls fanning a single pism subcommand out across the hosts
// in an ssh config.
type RunAllOptions struct {
	ConfigFile     string   // ssh config to enumerate + connect with ("" = ~/.ssh/config)
	RemoteBin      string   // pism path on the remotes (default "pism")
	Include        []string // glob patterns; empty = all hosts
	Exclude        []string // glob patterns to skip
	Sub            string   // remote pism subcommand to run (e.g. "update", "config")
	Args           []string // args passed after the subcommand (e.g. --pre, or a config key/value)
	ConnectTimeout int      // ssh ConnectTimeout seconds for the probe (0 = ssh default)
	TTY            bool     // allocate a pty on each remote (unused for update/config)
}

// RunAll enumerates hosts from the ssh config, filters them by include/exclude,
// probes each for a pism binary, and runs `pism <Sub> <Args...>` on the ones
// that have it. Hosts that are unreachable or lack pism are skipped with a note
// (not treated as failures). Returns a non-zero code if any host's command
// failed. This powers both `pism update --all` and `pism config --all`.
func RunAll(o RunAllOptions) (int, error) {
	if o.Sub == "" {
		return 1, fmt.Errorf("RunAll: empty subcommand")
	}
	hosts, err := ListConfigHosts(o.ConfigFile)
	if err != nil {
		return 1, fmt.Errorf("read ssh config: %w", err)
	}
	if len(hosts) == 0 {
		return 0, fmt.Errorf("no hosts found in ssh config")
	}

	var targets []string
	for _, h := range hosts {
		if len(o.Include) > 0 && !MatchesAny(h, o.Include) {
			continue
		}
		if MatchesAny(h, o.Exclude) {
			continue
		}
		targets = append(targets, h)
	}
	if len(targets) == 0 {
		return 0, fmt.Errorf("no hosts matched the include/exclude filters")
	}

	fmt.Fprintf(os.Stderr, "pism %s: %d host(s): %s\n", o.Sub, len(targets), strings.Join(targets, ", "))

	rc := 0
	var ran, skipped, failed int
	for _, h := range targets {
		has, perr := hasPism(h, o.ConfigFile, o.RemoteBin, o.ConnectTimeout)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "  [%s] skip: unreachable over ssh\n", h)
			skipped++
			continue
		}
		if !has {
			fmt.Fprintf(os.Stderr, "  [%s] skip: pism not installed\n", h)
			skipped++
			continue
		}
		fmt.Fprintf(os.Stderr, "  [%s] %s…\n", h, o.Sub)
		code, ferr := Forward(Options{Host: h, RemoteBin: o.RemoteBin, ConfigFile: o.ConfigFile, TTY: o.TTY},
			append([]string{o.Sub}, o.Args...))
		if ferr != nil || code != 0 {
			fmt.Fprintf(os.Stderr, "  [%s] %s FAILED (exit %d)\n", h, o.Sub, code)
			failed++
			rc = 1
			continue
		}
		ran++
	}
	fmt.Fprintf(os.Stderr, "pism %s: %d ok, %d skipped, %d failed\n", o.Sub, ran, skipped, failed)
	return rc, nil
}

// ProxyConn is a client-side handle to a remote holder reached over ssh: it
// reads from the ssh process's stdout and writes to its stdin, so the pism
// frame protocol runs end-to-end between the local client and the remote
// holder (the remote runs `pism __attach-proxy`). Close tears down the pipes
// and waits for ssh to exit.
type ProxyConn struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	cmd    *exec.Cmd
}

func (p *ProxyConn) Read(b []byte) (int, error)  { return p.stdout.Read(b) }
func (p *ProxyConn) Write(b []byte) (int, error) { return p.stdin.Write(b) }

func (p *ProxyConn) Close() error {
	_ = p.stdin.Close()
	_ = p.stdout.Close()
	return p.cmd.Wait()
}

// AttachProxy starts `ssh [-F cfg] host <bin> __attach-proxy --id <id>` with
// binary stdin/stdout pipes (no pty) and returns a ProxyConn the local client
// can run the attach frame protocol over. ssh's stderr is inherited so remote
// errors surface. The caller must Close the returned conn.
func AttachProxy(o Options, id string) (*ProxyConn, error) {
	bin := o.RemoteBin
	if bin == "" {
		bin = "pism"
	}
	sshArgs := configArgs(o.ConfigFile)
	// No -t: we want a clean binary byte pipe, not a pty with escape handling.
	sshArgs = append(sshArgs, o.Host, bin, "__attach-proxy", "--id", id)
	cmd := exec.Command("ssh", sshArgs...)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &ProxyConn{stdin: stdin, stdout: stdout, cmd: cmd}, nil
}

// HostResult is the outcome of running a pism subcommand on one host as part
// of a capture fan-out (GatherAll).
type HostResult struct {
	Host    string
	Stdout  []byte
	Skipped bool   // host was unreachable or lacked pism
	Reason  string // why it was skipped (for a note)
	Err     error  // command ran but failed
}

// GatherAll enumerates + filters hosts like RunAll, probes each for pism, and
// CAPTURES the stdout of `pism <Sub> <Args...>` on the reachable ones (instead
// of streaming it). It powers aggregating commands such as `pism ls --all`.
// Hosts that are unreachable or lack pism are returned as Skipped, not errors.
func GatherAll(o RunAllOptions) ([]HostResult, error) {
	if o.Sub == "" {
		return nil, fmt.Errorf("GatherAll: empty subcommand")
	}
	hosts, err := ListConfigHosts(o.ConfigFile)
	if err != nil {
		return nil, fmt.Errorf("read ssh config: %w", err)
	}
	if len(hosts) == 0 {
		return nil, fmt.Errorf("no hosts found in ssh config")
	}
	var targets []string
	for _, h := range hosts {
		if len(o.Include) > 0 && !MatchesAny(h, o.Include) {
			continue
		}
		if MatchesAny(h, o.Exclude) {
			continue
		}
		targets = append(targets, h)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no hosts matched the include/exclude filters")
	}

	results := make([]HostResult, 0, len(targets))
	for _, h := range targets {
		has, perr := hasPism(h, o.ConfigFile, o.RemoteBin, o.ConnectTimeout)
		if perr != nil {
			results = append(results, HostResult{Host: h, Skipped: true, Reason: "unreachable over ssh"})
			continue
		}
		if !has {
			results = append(results, HostResult{Host: h, Skipped: true, Reason: "pism not installed"})
			continue
		}
		out, rerr := capture(h, o.ConfigFile, o.RemoteBin, o.ConnectTimeout, append([]string{o.Sub}, o.Args...))
		results = append(results, HostResult{Host: h, Stdout: out, Err: rerr})
	}
	return results, nil
}

// Capture runs `ssh [-F cfg] host <bin> <args...>` and returns its stdout. It's
// the exported single-host counterpart to GatherAll, used for quick queries
// like fetching a host's `ls --porcelain` during cross-host switching.
func Capture(o Options, timeout int, args []string) ([]byte, error) {
	return capture(o.Host, o.ConfigFile, o.RemoteBin, timeout, args)
}

// capture runs `ssh [-F cfg] host <bin> <args...>` and returns its stdout.
func capture(host, cfg, remoteBin string, timeout int, args []string) ([]byte, error) {
	bin := remoteBin
	if bin == "" {
		bin = "pism"
	}
	sshArgs := configArgs(cfg)
	sshArgs = append(sshArgs, "-o", "BatchMode=yes")
	if timeout > 0 {
		sshArgs = append(sshArgs, "-o", fmt.Sprintf("ConnectTimeout=%d", timeout))
	}
	sshArgs = append(sshArgs, host, bin)
	sshArgs = append(sshArgs, args...)
	return exec.Command("ssh", sshArgs...).Output()
}

// hasPism probes a host for a usable pism binary over ssh. It uses BatchMode so
// a host needing a password is skipped rather than hanging on a prompt.
func hasPism(host, cfg, remoteBin string, timeout int) (bool, error) {
	bin := remoteBin
	if bin == "" {
		bin = "pism"
	}
	args := configArgs(cfg)
	args = append(args, "-o", "BatchMode=yes")
	if timeout > 0 {
		args = append(args, "-o", fmt.Sprintf("ConnectTimeout=%d", timeout))
	}
	args = append(args, host, "command -v "+shellQuote(bin)+" >/dev/null 2>&1 && echo PISM_YES || echo PISM_NO")
	out, err := exec.Command("ssh", args...).Output()
	if err != nil {
		return false, err // 255 = unreachable/auth; treat as skip
	}
	return strings.Contains(string(out), "PISM_YES"), nil
}

// ensureRemotePath appends ~/.local/bin to the remote shell rc files that
// non-interactive ssh sessions read, idempotently. The script is fed to a
// remote sh via stdin to avoid nested-quoting problems.
func ensureRemotePath(cfg, host string) {
	const script = "set -e\n" +
		"dir=\"$HOME/.local/bin\"\n" +
		"marker='# added by pism installer'\n" +
		"ensure() {\n" +
		"  f=\"$1\"\n" +
		"  if [ ! -e \"$f\" ]; then case \"$f\" in *.zshenv) : ;; *) return 0 ;; esac; fi\n" +
		"  if grep -qs \"$marker\" \"$f\" 2>/dev/null; then return 0; fi\n" +
		"  { printf '\\n%s\\n' \"$marker\"; printf 'case \":$PATH:\" in *\":%s:\"*) ;; *) export PATH=\"%s:$PATH\" ;; esac\\n' \"$dir\" \"$dir\"; } >> \"$f\"\n" +
		"  printf 'pism install: PATH configured in %s\\n' \"$f\" >&2\n" +
		"}\n" +
		"ensure \"$HOME/.zshenv\"\n" +
		"ensure \"$HOME/.bashrc\"\n" +
		"ensure \"$HOME/.profile\"\n"

	c := exec.Command("ssh", append(configArgs(cfg), host, "sh", "-s")...)
	c.Stdin = strings.NewReader(script)
	c.Stdout, c.Stderr = os.Stderr, os.Stderr
	_ = c.Run()
	// Verify pism is now resolvable in a fresh non-interactive shell.
	if out, err := exec.Command("ssh", append(configArgs(cfg), host, "command -v pism || true")...).Output(); err == nil {
		if strings.TrimSpace(string(out)) == "" {
			fmt.Fprintf(os.Stderr, "pism install: note \u2014 pism still not on the remote non-interactive PATH; use --remote-bin ~/.local/bin/pism if needed\n")
		}
	}
}

// Install bootstraps pism onto a remote host over ssh by running the published
// installer for the detected OS. POSIX hosts get the shell installer via
// curl|sh (wget fallback); Windows hosts get the PowerShell installer. It works
// from any client OS (only the system ssh is used).
//
//	shURL/ps1URL  the raw install script URLs
//	version       optional release tag to pin ("" = latest stable)
func Install(host, cfg, shURL, ps1URL, version string) error {
	ssh := func(tty bool, a ...string) *exec.Cmd {
		args := configArgs(cfg)
		if tty {
			args = append(args, "-t")
		}
		c := exec.Command("ssh", append(args, append([]string{host}, a...)...)...)
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c
	}

	// Detect the remote OS. `uname -s` succeeds on Linux/macOS. On Windows it
	// isn't found (the shell returns non-zero but ssh still CONNECTED). ssh's
	// own errors (unreachable host, auth) use exit code 255 — distinguish those
	// so we don't mistake an unreachable host for Windows.
	detect := exec.Command("ssh", append(configArgs(cfg), host, "uname -s")...)
	var dout, derr bytes.Buffer
	detect.Stdout, detect.Stderr = &dout, &derr
	runErr := detect.Run()
	osName := strings.TrimSpace(dout.String())
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok && ee.ExitCode() == 255 {
			return fmt.Errorf("cannot reach %s over ssh: %s", host, strings.TrimSpace(derr.String()))
		}
		// connected, but `uname` failed -> treat as Windows below
	}

	switch {
	case osName == "Linux" || osName == "Darwin" || strings.Contains(osName, "BSD"):
		fmt.Fprintf(os.Stderr, "pism install: %s detected on %s \u2014 running the shell installer\n", osName, host)
		env := ""
		if version != "" {
			env = "PISM_VERSION=" + shellQuote(version) + " "
		}
		dl := "if command -v curl >/dev/null 2>&1; then curl -fsSL " + shellQuote(shURL) +
			"; else wget -qO- " + shellQuote(shURL) + "; fi"
		remote := env + "sh -c \"$(" + dl + ")\""
		if err := ssh(false, remote).Run(); err != nil {
			return fmt.Errorf("remote install failed: %w", err)
		}
		// Make ~/.local/bin resolvable for future non-interactive ssh commands
		// (so `pism <host> ...` finds pism). zsh reads ~/.zshenv for every
		// invocation; bash/sh get ~/.bashrc / ~/.profile when present.
		ensureRemotePath(cfg, host)
	default:
		// Assume Windows: run the PowerShell installer.
		fmt.Fprintf(os.Stderr, "pism install: assuming Windows on %s \u2014 running the PowerShell installer\n", host)
		setVer := ""
		if version != "" {
			setVer = "$env:PISM_VERSION='" + version + "'; "
		}
		ps := "powershell -NoProfile -ExecutionPolicy Bypass -Command \"" + setVer +
			"irm " + ps1URL + " | iex\""
		if err := ssh(false, ps).Run(); err != nil {
			return fmt.Errorf("remote install failed (is this a Windows host with PowerShell + OpenSSH?): %w", err)
		}
	}
	fmt.Fprintf(os.Stderr, "pism install: done. If the remote shell can't find pism, add its\n"+
		"install dir to PATH (see the installer output), or use --remote-bin. Then:  pism %s ls\n", host)
	return nil
}
