package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/vandlol/pism/internal/config"
	"github.com/vandlol/pism/internal/remote"
	"github.com/vandlol/pism/internal/session"
	"github.com/vandlol/pism/internal/update"
)

// Raw install script URLs (served from the default branch on GitHub).
const (
	installShURL  = "https://raw.githubusercontent.com/vandlol/pism/main/scripts/install.sh"
	installPs1URL = "https://raw.githubusercontent.com/vandlol/pism/main/scripts/install.ps1"
)

// runInstall bootstraps pism onto a remote host over ssh (auto-detecting the
// remote OS). Used by both `pism <host> install` and `pism install <host>`.
func runInstall(g globals, host string, args []string) int {
	version := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--version" && i+1 < len(args) {
			version = args[i+1]
			i++
		}
	}
	if err := remote.Install(host, remote.ResolveConfig(g.sshConfig), installShURL, installPs1URL, version); err != nil {
		fmt.Fprintln(os.Stderr, "pism install:", err)
		return 1
	}
	return 0
}

// cmdLogs prints a session's holder log (verbose diagnostics land here).
func cmdLogs(g globals, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: pism logs <id>")
		return 2
	}
	m, err := session.Load(args[0])
	path := ""
	if err == nil {
		path = session.LogPath(m.ID)
	} else {
		// allow dumping by a raw/full id even if metadata is already gone
		path = session.LogPath(args[0])
	}
	b, rerr := os.ReadFile(path)
	if rerr != nil {
		fmt.Fprintf(os.Stderr, "pism logs: %v\n", rerr)
		return 1
	}
	os.Stdout.Write(b)
	return 0
}

// cmdInstall handles `pism install <host> [flags]`.
func cmdInstall(g globals, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: pism install <host>   (or: pism <host> install)")
		return 2
	}
	return runInstall(g, args[0], args[1:])
}

// applyConfig layers config-file values under the built-in defaults (command
// line flags, parsed later, override these).
func applyConfig(g *globals, cfg *config.Config) {
	if v, ok := cfg.Get("pi"); ok {
		g.pi = v
	}
	if v, ok := cfg.Get("detach-key"); ok {
		g.detachStr = v
	}
	if v, ok := cfg.Get("remote-bin"); ok {
		g.remoteBin = v
	}
	if v, ok := cfg.Get("ssh-config"); ok {
		g.sshConfig = v
	}
	g.topicLen = cfg.GetInt("topic-len", g.topicLen)
	if v, ok := cfg.Get("ready-timeout"); ok {
		g.readyTimeout = parseWait(v)
	}
}

// parseWait converts a wait spec into a duration. "0", "forever", "inf",
// "infinite" or "none" mean wait indefinitely (0). Accepts Go durations
// (30s, 5m) and bare integers (seconds).
func parseWait(s string) time.Duration {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "0", "forever", "inf", "infinite", "none", "wait":
		return 0
	}
	if d, err := time.ParseDuration(s); err == nil {
		if d < 0 {
			return 0
		}
		return d
	}
	if n, err := strconv.Atoi(s); err == nil {
		if n <= 0 {
			return 0
		}
		return time.Duration(n) * time.Second
	}
	return 30 * time.Second
}

// cmdUpdate replaces the running binary with a fresh build from the update
// server (this Mac by default; override with the update-url config key,
// $PISM_UPDATE_URL, or --update-url).
func cmdUpdate(args []string) int {
	base := os.Getenv("PISM_UPDATE_URL")
	channel := os.Getenv("PISM_UPDATE_CHANNEL")
	if cfg, err := config.Load(); err == nil {
		if v, ok := cfg.Get("update-url"); ok && base == "" {
			base = v
		}
		if v, ok := cfg.Get("update-channel"); ok && channel == "" {
			channel = v
		}
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--update-url":
			if i+1 < len(args) {
				base = args[i+1]
				i++
			}
		case "--channel":
			if i+1 < len(args) {
				channel = args[i+1]
				i++
			}
		case "--pre", "-pre", "--unstable", "-unstable", "--dev", "-dev", "--nightly", "-nightly":
			channel = "unstable"
		case "--stable", "-stable":
			channel = "stable"
		}
	}
	if err := update.Run(update.Options{
		CurrentVersion: version,
		Channel:        update.NormalizeChannel(channel),
		BaseURL:        base,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "pism update:", err)
		return 1
	}
	return 0
}

// cmdConfig implements the git-style `pism config` command.
func cmdConfig(args []string) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "pism config:", err)
	}
	if args == nil || len(args) == 0 {
		// Show ALL known keys with their current value (or "unset") + help.
		width := 0
		for _, k := range config.Keys {
			if len(k.Name) > width {
				width = len(k.Name)
			}
		}
		for _, k := range config.Keys {
			val, ok := cfg.Get(k.Name)
			if !ok {
				val = "(unset)"
			}
			fmt.Printf("%-*s = %-10s  # %s\n", width, k.Name, val, k.Desc)
		}
		return 0
	}
	if args[0] == "--list" || args[0] == "-l" {
		// git-style: only keys that are actually set
		for _, kv := range cfg.All() {
			fmt.Printf("%s=%s\n", kv[0], kv[1])
		}
		return 0
	}
	switch args[0] {
	case "--path", "-p":
		fmt.Println(config.Path())
		return 0
	case "--unset":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: pism config --unset <key>")
			return 2
		}
		if err := cfg.Unset(args[1]); err != nil {
			fmt.Fprintln(os.Stderr, "pism config:", err)
			return 1
		}
		return 0
	}
	key := args[0]
	if len(args) == 1 {
		if v, ok := cfg.Get(key); ok {
			fmt.Println(v)
			return 0
		}
		return 1 // unset key -> non-zero, like git
	}
	val := strings.Join(args[1:], " ")
	if err := cfg.Set(key, val); err != nil {
		fmt.Fprintln(os.Stderr, "pism config:", err)
		return 2
	}
	return 0
}

// extractGlobals pulls global --flags from anywhere in argv (before or after
// the subcommand) and returns the remaining args (subcommand + its args).
func extractGlobals(argv []string, g *globals) ([]string, error) {
	var rest []string
	takeVal := func(i *int, name string) (string, error) {
		if *i+1 >= len(argv) {
			return "", fmt.Errorf("flag %s needs a value", name)
		}
		*i++
		return argv[*i], nil
	}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		// Stop pulling globals once we hit a bare "--" passthrough marker,
		// so `pism new -- pi-args...` keeps pi args intact.
		if a == "--" {
			rest = append(rest, argv[i:]...)
			break
		}
		// -v / -vv / -vvv : verbosity by counting v's
		if len(a) >= 2 && a[0] == '-' && a[1] == 'v' && strings.Trim(a, "-v") == "" {
			g.verbosity = len(a) - 1
			continue
		}
		key, inlineVal, hasInline := splitFlag(a)
		var err error
		val := inlineVal
		get := func(name string) (string, error) {
			if hasInline {
				return val, nil
			}
			return takeVal(&i, name)
		}
		switch key {
		case "--remote-bin":
			if g.remoteBin, err = get(key); err != nil {
				return nil, err
			}
		case "--ssh-config":
			if g.sshConfig, err = get(key); err != nil {
				return nil, err
			}
		case "--pi":
			if g.pi, err = get(key); err != nil {
				return nil, err
			}
			g.setPi = true
		case "--detach-key":
			if g.detachStr, err = get(key); err != nil {
				return nil, err
			}
			g.setDetach = true
		case "--topic-len":
			s, e := get(key)
			if e != nil {
				return nil, e
			}
			n, e2 := strconv.Atoi(s)
			if e2 != nil {
				return nil, fmt.Errorf("--topic-len: %v", e2)
			}
			g.topicLen = n
			g.setTopicLen = true
		case "--verbose":
			s, e := get(key)
			if e != nil {
				return nil, e
			}
			n, e2 := strconv.Atoi(s)
			if e2 != nil {
				return nil, fmt.Errorf("--verbose: %v", e2)
			}
			g.verbosity = n
		case "--dist":
			if g.dist, err = get(key); err != nil {
				return nil, err
			}
		default:
			rest = append(rest, a)
		}
	}
	return rest, nil
}

func splitFlag(a string) (key, val string, hasInline bool) {
	if !strings.HasPrefix(a, "--") {
		return a, "", false
	}
	if i := strings.IndexByte(a, '='); i >= 0 {
		return a[:i], a[i+1:], true
	}
	return a, "", false
}

// parseDetach turns a human detach-key spec into a byte sequence. An empty
// result means "disabled". Accepts:
//   - "none"/"off"/"disable"       -> disabled
//   - "^\\" or "ctrl-\\"            -> control byte (caret/ctrl + char)
//   - a decimal code (0-255)       -> that byte
//   - a function-key name f1..f20  -> the terminal escape sequence for it
//   - a raw escape spec (\x1b..., \e..., esc...) -> those literal bytes
//   - a single literal char        -> that char
//
// Unrecognized specs fall back to the default and print a warning, so a typo
// no longer silently leaves you on Ctrl-\ with no clue why.
func parseDetach(s string) []byte {
	s = strings.TrimSpace(s)
	lower := strings.ToLower(s)
	switch lower {
	case "", "none", "off", "disable", "disabled":
		return nil
	}
	if seq, ok := functionKeys[lower]; ok {
		return []byte(seq)
	}
	if seq, ok := parseEscapeSpec(s); ok {
		return seq
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 && n < 256 {
		return []byte{byte(n)}
	}
	if strings.HasPrefix(lower, "ctrl-") && len(s) == 6 {
		return []byte{ctrlByte(s[5])}
	}
	if strings.HasPrefix(s, "^") && len(s) == 2 {
		return []byte{ctrlByte(s[1])}
	}
	if len(s) == 1 {
		return []byte{s[0]}
	}
	fmt.Fprintf(os.Stderr, "pism: unrecognized detach-key %q; using default (Ctrl-\\)\n", s)
	return []byte{client_DefaultDetach}
}

// functionKeys maps function-key names to the escape sequences xterm-family
// terminals (xterm-256color, iTerm, Windows Terminal) emit for them. F13-F16
// use the modern CSI-modifier form (Shift+F1..F4) which matches current
// terminfo; F17-F20 use the tilde form.
var functionKeys = map[string]string{
	"f1": "\x1bOP", "f2": "\x1bOQ", "f3": "\x1bOR", "f4": "\x1bOS",
	"f5": "\x1b[15~", "f6": "\x1b[17~", "f7": "\x1b[18~", "f8": "\x1b[19~",
	"f9": "\x1b[20~", "f10": "\x1b[21~", "f11": "\x1b[23~", "f12": "\x1b[24~",
	"f13": "\x1b[1;2P", "f14": "\x1b[1;2Q", "f15": "\x1b[1;2R", "f16": "\x1b[1;2S",
	"f17": "\x1b[15;2~", "f18": "\x1b[17;2~", "f19": "\x1b[18;2~", "f20": "\x1b[19;2~",
}

// parseEscapeSpec accepts a literal escape sequence written as text, so users
// whose terminal emits something different can set the exact bytes. Understands
// a leading \x1b, \e, \033, or the word "esc" as ESC (0x1b), plus \xNN hex
// escapes anywhere in the string.
func parseEscapeSpec(s string) ([]byte, bool) {
	low := strings.ToLower(s)
	var rest string
	switch {
	case strings.HasPrefix(low, "\\x1b"):
		rest = s[4:]
	case strings.HasPrefix(low, "\\033"):
		rest = s[4:]
	case strings.HasPrefix(low, "\\e"):
		rest = s[2:]
	case strings.HasPrefix(low, "esc"):
		rest = s[3:]
	default:
		return nil, false
	}
	out := []byte{0x1b}
	for i := 0; i < len(rest); {
		if rest[i] == '\\' && i+4 <= len(rest) && (rest[i+1] == 'x' || rest[i+1] == 'X') {
			if n, err := strconv.ParseUint(rest[i+2:i+4], 16, 8); err == nil {
				out = append(out, byte(n))
				i += 4
				continue
			}
		}
		out = append(out, rest[i])
		i++
	}
	return out, true
}

// ctrlByte maps a printable char to its control code (e.g. '\\' -> 0x1c).
func ctrlByte(c byte) byte {
	up := c
	if up >= 'a' && up <= 'z' {
		up -= 32
	}
	return up & 0x1f
}

const client_DefaultDetach = 0x1c // Ctrl-\

// isCommand reports whether tok is a local pism subcommand (so the first
// positional arg can be disambiguated from a host).
func isCommand(tok string) bool {
	switch tok {
	case "new", "n", "ls", "list", "attach", "a", "kill", "k", "gc", "topic":
		return true
	}
	return false
}

// isRemotable reports whether a subcommand can be run against a remote host.
func isRemotable(tok string) bool {
	switch tok {
	case "new", "n", "ls", "list", "attach", "a", "kill", "k", "gc", "topic":
		return true
	}
	return false
}

func hasFlag(args []string, f string) bool {
	for _, a := range args {
		if a == f {
			return true
		}
	}
	return false
}

func absPath(p string) (string, error) { return filepath.Abs(p) }

func cmdBuildAll(g globals, _ []string) int {
	targets := [][2]string{
		{"linux", "amd64"}, {"linux", "arm64"},
		{"darwin", "amd64"}, {"darwin", "arm64"},
		{"windows", "amd64"}, {"windows", "arm64"},
	}
	if err := os.MkdirAll(g.dist, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "build-all:", err)
		return 1
	}
	rc := 0
	for _, t := range targets {
		goos, goarch := t[0], t[1]
		out := filepath.Join(g.dist, fmt.Sprintf("pism-%s-%s", goos, goarch))
		if goos == "windows" {
			out += ".exe"
		}
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", out, ".")
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
		cmd.Stderr = os.Stderr
		fmt.Fprintf(os.Stderr, "building %s/%s -> %s\n", goos, goarch, out)
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "  FAILED: %v\n", err)
			rc = 1
		}
	}
	return rc
}

func usage() {
	fmt.Fprint(os.Stderr, `pism — cross-platform, tmux-free session manager for pi

USAGE
  pism [flags] <command> [args]          run locally
  pism [flags] <host> <command> [args]   run on a remote host over ssh
                                         (host is any ssh target or config alias)

COMMANDS
  new [dir] [-d] [-w <dur>] [-- pi args]  Start a session (attaches unless -d;
                                          -w/--wait sets ready timeout, 0=forever)
  ls                               List sessions with their topic + liveness
  attach <id>                      Re-attach to a session (detach: Ctrl-\ )
  kill <id> [id...]                Terminate session(s)
  gc                               Remove metadata for dead sessions
  topic <id>                       Print a session's topic (for scripts)
  config [key] [value]             Show all keys, or get/set (--list, --unset, --path)
  update [--pre|--stable]          Update pism in place (channel: stable|unstable)
  install <host>                   Install pism on a remote host over ssh
                                   (also: pism <host> install)
  logs <id>                        Print a session's holder log (diagnostics)
  push <host> [dest]               Copy the matching pism binary to a host
  build-all                        Cross-compile binaries into ./dist
  version

FLAGS
  --remote-bin <path>    pism path on the remote host (default: pism)
  --ssh-config <path>    ssh config file to use (-F). Auto-detects a local
                         ./ssh_config, ./.ssh/config or ./.pism/ssh_config;
                         else falls back to ssh's own ~/.ssh/config
  --pi <cmd>             Command used to launch pi (default: pi)
  --detach-key <spec>    Detach key: ^\ , ctrl-o, f16, a char, a code,
                         an escape seq (\x1b[29~), or "none"
  --topic-len <n>        Max topic width in ls (default: 40)
  --dist <dir>           Output/dir for build-all & push (default: dist)
  -v / -vv / -vvv        Verbosity: info / debug / trace (holder logs land in
                         the session log; see: pism logs <id>)

EXAMPLES
  pism new ~/proj                  start + attach a session in ~/proj
  pism ls                          see every session by topic
  pism attach 3f9a1c2b             reconnect (prefix ids ok)
  pism srv ls                      list sessions on host 'srv' over ssh
  pism srv attach 3f9a             attach to a remote session on 'srv'
  pism srv new ~/svc               start a session on 'srv'
`)
}
