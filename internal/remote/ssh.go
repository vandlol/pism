// Package remote forwards pism subcommands to a pism binary on another host
// over the system ssh client. It never handles credentials itself, relying on
// the user's existing ssh config / keys / agent.
//
// SSH config resolution: the system ssh already reads ~/.ssh/config. On top of
// that, pism will prefer a project-local config when present (or one given via
// --ssh-config / $PISM_SSH_CONFIG), passing it to ssh/scp with -F.
package remote

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
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
//   shURL/ps1URL  the raw install script URLs
//   version       optional release tag to pin ("" = latest stable)
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
