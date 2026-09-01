// Package remote forwards pism subcommands to a pism binary on another host
// over the system ssh client. It never handles credentials itself, relying on
// the user's existing ssh config / keys / agent.
//
// SSH config resolution: the system ssh already reads ~/.ssh/config. On top of
// that, pism will prefer a project-local config when present (or one given via
// --ssh-config / $PISM_SSH_CONFIG), passing it to ssh/scp with -F.
package remote

import (
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
