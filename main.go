// Command pism is a cross-platform (Linux/macOS/Windows), tmux-free session
// manager for pi. It keeps pi sessions alive in detached PTY holders, lets you
// re-attach reliably (locally or over ssh), and lists sessions by their topic.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/vandlol/pism/internal/client"
	"github.com/vandlol/pism/internal/config"
	"github.com/vandlol/pism/internal/dbg"
	"github.com/vandlol/pism/internal/holder"
	"github.com/vandlol/pism/internal/manager"
	"github.com/vandlol/pism/internal/remote"
	"github.com/vandlol/pism/internal/session"
	"github.com/vandlol/pism/internal/ui"
)

var version = "dev" // overridden at build time via -ldflags "-X main.version=..."

type globals struct {
	host       string
	remoteBin  string
	pi         string
	detachStr  string
	switchPrev string
	switchNext string
	sshConfig  string
	topicLen   int

	readyTimeout time.Duration
	verbosity    int

	setPi       bool
	setDetach   bool
	setSwitch   bool
	setTopicLen bool
	dist        string
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	// __holder is the detached runtime; handle it before global parsing.
	if len(argv) > 0 && argv[0] == "__holder" {
		return runHolder(argv[1:])
	}
	// __attach-proxy is the remote half of a cross-host attach: it bridges this
	// ssh session's stdio to a local holder socket. Handle it before global
	// parsing (it takes only --id and speaks the raw frame protocol on stdio).
	if len(argv) > 0 && argv[0] == "__attach-proxy" {
		return runAttachProxy(argv[1:])
	}

	g := globals{pi: "pi", detachStr: "^\\", switchPrev: "ctrl-left", switchNext: "ctrl-right", topicLen: 40, dist: "dist", readyTimeout: 30 * time.Second}
	// Load config (creates the file on first run) and apply it as defaults
	// beneath any command-line flags.
	if cfg, cerr := config.Load(); cerr == nil {
		applyConfig(&g, cfg)
	}
	rest, err := extractGlobals(argv, &g)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pism:", err)
		return 2
	}
	dbg.SetLevel(g.verbosity)
	if len(rest) == 0 {
		usage()
		return 1
	}

	cmd, args := rest[0], rest[1:]

	// Management/meta commands are always local and never host-scoped.
	switch cmd {
	case "help", "-h", "--help":
		usage()
		return 0
	case "version", "--version":
		fmt.Println("pism", version)
		return 0
	case "push":
		return cmdPush(g, args)
	case "build-all":
		return cmdBuildAll(g, args)
	case "config":
		return cmdConfig(g, args)
	case "update", "self-update":
		return cmdUpdate(g, args)
	case "install":
		// pism install <host> [flags]
		return cmdInstall(g, args)
	case "logs", "log":
		return cmdLogs(g, args)
	}

	// Grammar: the first token is a HOST unless it is a known command.
	//   pism <command> [args]          -> local
	//   pism <host> <command> [args]   -> remote over ssh
	if !isCommand(cmd) {
		host := cmd
		if len(args) == 0 {
			fmt.Fprintf(os.Stderr, "pism: %q is not a command; for a remote host use:  pism %s <command>\n", host, host)
			usage()
			return 2
		}
		sub, subArgs := args[0], args[1:]
		if sub == "install" {
			// pism <host> install [flags] — bootstrap pism onto the host over
			// ssh (the remote has no pism yet, so this is NOT forwarded).
			return runInstall(g, host, subArgs)
		}
		if sub == "attach" || sub == "a" {
			// Attach to a remote session from the LOCAL client via an ssh
			// proxy, so detach/switch keys are handled here, not on the remote.
			return cmdAttachRemote(g, host, subArgs)
		}
		if !isRemotable(sub) {
			fmt.Fprintf(os.Stderr, "pism: %q cannot run on a remote host\n", sub)
			return 2
		}
		g.host = host
		return forward(g, sub, subArgs)
	}

	switch cmd {
	case "new", "n":
		return cmdNew(g, args)
	case "ls", "list":
		return cmdLs(g, args)
	case "attach", "a":
		return cmdAttach(g, args)
	case "kill", "k":
		return cmdKill(g, args)
	case "gc":
		return cmdGC(g)
	case "topic":
		return cmdTopic(g, args)
	case "name", "rename":
		return cmdRename(g, args)
	default:
		fmt.Fprintf(os.Stderr, "pism: unknown command %q\n", cmd)
		usage()
		return 2
	}
}

func runAttachProxy(args []string) int {
	var id string
	for i := 0; i < len(args); i++ {
		if args[i] == "--id" && i+1 < len(args) {
			i++
			id = args[i]
		}
	}
	if id == "" {
		fmt.Fprintln(os.Stderr, "attach-proxy: missing --id")
		return 2
	}
	m, err := session.Load(id)
	if err != nil {
		fmt.Fprintln(os.Stderr, "attach-proxy:", err)
		return 1
	}
	if err := client.Proxy(m, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "attach-proxy:", err)
		return 1
	}
	return 0
}

func runHolder(args []string) int {
	var id, name, cwd, pi string
	var extra []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--id":
			i++
			id = args[i]
		case "--name":
			i++
			name = args[i]
		case "--cwd":
			i++
			cwd = args[i]
		case "--pi":
			i++
			pi = args[i]
		case "--":
			extra = append(extra, args[i+1:]...)
			i = len(args)
		}
	}
	if id == "" || pi == "" {
		fmt.Fprintln(os.Stderr, "holder: missing --id/--pi")
		return 2
	}
	if err := holder.Run(holder.Config{ID: id, Name: name, Cwd: cwd, PiCmd: pi, ExtraArgs: extra}); err != nil {
		fmt.Fprintln(os.Stderr, "holder:", err)
		return 1
	}
	return 0
}

func cmdNew(g globals, args []string) int {
	detached := false
	var dir string
	var piArgs []string
	waitSpec := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-d" || a == "--detached":
			detached = true
		case a == "--wait" || a == "-w":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "pism new: --wait needs a value (e.g. 30s, 5m, 0=forever)")
				return 2
			}
			i++
			waitSpec = args[i]
		case a == "--":
			piArgs = append(piArgs, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "pism new: unknown flag %q\n", a)
			return 2
		default:
			if dir == "" {
				dir = a
			}
		}
	}
	readyTimeout := g.readyTimeout
	if waitSpec != "" {
		readyTimeout = parseWait(waitSpec)
	}
	if dir == "" {
		dir, _ = os.Getwd()
	} else {
		if abs, err := absPath(dir); err == nil {
			dir = abs
		}
	}
	id, err := holder.Launch(dir, g.pi, piArgs, readyTimeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pism new:", err)
		if id != "" {
			fmt.Fprintln(os.Stderr, "  session id:", id)
		}
		return 1
	}
	name := id[:8]
	if m, lerr := session.Load(id); lerr == nil && m.Name != "" {
		name = m.Name
	}
	if detached {
		fmt.Fprintf(os.Stderr, "[started %s]\n", name)
		fmt.Println(id) // id to stdout for scripts
		return 0
	}
	fmt.Fprintf(os.Stderr, "[attached %s — detach with %s]\n", name, g.detachStr)
	return attachByID(g, id)
}

func cmdLs(g globals, args []string) int {
	porcelain := false
	all := false
	for _, a := range args {
		switch a {
		case "--porcelain":
			porcelain = true
		case "--all", "--hosts", "--include", "--exclude", "--connect-timeout":
			all = true
		}
	}
	if all {
		return cmdLsAll(g, args)
	}
	topicLen := g.topicLen
	if porcelain {
		topicLen = 1 << 20 // emit full topics; the consumer truncates for display
	}
	rows, err := manager.Rows(topicLen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pism ls:", err)
		return 1
	}
	if porcelain {
		ui.RenderPorcelain(os.Stdout, rows)
		return 0
	}
	ui.Render(os.Stdout, rows)
	return 0
}

func cmdAttach(g globals, args []string) int {
	id := firstPositional(args)
	if hasAllHostsMode(args) {
		// Cross-host mode: attach and let Ctrl-Left/Right switch across hosts.
		sel := parseAllHostsFlags(args)
		start, ok := resolveStart(g, id, sel)
		if !ok {
			return 1
		}
		return orchestrate(g, start, sel)
	}
	if id == "" {
		// No id: resume the most recently created live session.
		m, err := manager.NewestLive()
		if err != nil {
			fmt.Fprintln(os.Stderr, "pism attach:", err)
			return 1
		}
		return attachByID(g, m.ID)
	}
	return attachByID(g, id)
}

// firstPositional returns the first non-flag argument (and skips a value that
// belongs to a preceding value-taking selection flag).
func firstPositional(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			key, _, inline := splitFlag(a)
			if !inline {
				switch key {
				case "--include", "--exclude", "--connect-timeout":
					i++ // consume the value
				}
			}
			continue
		}
		return a
	}
	return ""
}

// resolveStart picks the starting target for a cross-host attach: the given id
// (resolved locally first, then across the universe), or the newest live
// session anywhere when id is empty.
func resolveStart(g globals, id string, sel allHostsSel) (xtarget, bool) {
	if id == "" {
		uni := buildUniverse(g, sel)
		if len(uni) == 0 {
			fmt.Fprintln(os.Stderr, "pism attach: no live sessions on any host")
			return xtarget{}, false
		}
		return uni[0], true
	}
	if m, err := session.Load(id); err == nil {
		return xtarget{Host: localHost, ID: m.ID}, true
	}
	// Not local: look for it across hosts.
	uni := buildUniverse(g, sel)
	if i := indexOf(uni, xtarget{Host: "", ID: id}); i >= 0 {
		return uni[i], true
	}
	for _, t := range uni {
		if strings.HasPrefix(t.ID, id) {
			return t, true
		}
	}
	fmt.Fprintf(os.Stderr, "pism attach: no live session matching %q on any host\n", id)
	return xtarget{}, false
}

func attachByID(g globals, idOrPrefix string) int {
	m, err := session.Load(idOrPrefix)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pism attach:", err)
		return 1
	}
	keys := client.Keys{
		Detach:     parseDetach(g.detachStr),
		SwitchPrev: parseSwitch(g.switchPrev),
		SwitchNext: parseSwitch(g.switchNext),
	}
	for {
		outcome, err := client.Attach(m, keys)
		if err != nil {
			if ee, ok := err.(*client.ExitError); ok {
				return ee.Code
			}
			fmt.Fprintln(os.Stderr, "pism attach:", err)
			return 1
		}
		dir := 0
		switch outcome {
		case client.OutcomeSwitchPrev:
			dir = -1
		case client.OutcomeSwitchNext:
			dir = 1
		default:
			return 0
		}
		next, nerr := manager.AdjacentLive(m.ID, dir)
		if nerr != nil {
			fmt.Fprintln(os.Stderr, "pism attach:", nerr)
			return 1
		}
		m = next
		fmt.Fprintf(os.Stderr, "[switching to %s]\n", session.Topic(m.ID, m.Cwd, g.topicLen))
	}
}

func cmdKill(g globals, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: pism kill <id> [id...]")
		return 2
	}
	rc := 0
	for _, id := range args {
		if err := manager.Kill(id); err != nil {
			fmt.Fprintln(os.Stderr, "pism kill:", err)
			rc = 1
		} else {
			fmt.Fprintln(os.Stderr, "killed", id)
		}
	}
	return rc
}

func cmdGC(_ globals) int {
	n, err := manager.GC()
	if err != nil {
		fmt.Fprintln(os.Stderr, "pism gc:", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "removed %d dead session(s)\n", n)
	return 0
}

func cmdTopic(g globals, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: pism topic <id>")
		return 2
	}
	m, err := session.Load(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "pism topic:", err)
		return 1
	}
	fmt.Println(session.Topic(m.ID, m.Cwd, g.topicLen))
	return 0
}

func cmdPush(g globals, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: pism push <host> [dest-path]")
		return 2
	}
	dest := ""
	if len(args) > 1 {
		dest = args[1]
	}
	if err := remote.Push(args[0], g.dist, dest, remote.ResolveConfig(g.sshConfig)); err != nil {
		fmt.Fprintln(os.Stderr, "pism push:", err)
		return 1
	}
	return 0
}

func forward(g globals, cmd string, args []string) int {
	fwd := append([]string{cmd}, args...)
	// carry explicitly-set globals to the remote
	if g.setPi {
		fwd = append(fwd, "--pi", g.pi)
	}
	if g.setDetach && cmd == "attach" {
		fwd = append(fwd, "--detach-key", g.detachStr)
	}
	if g.setSwitch && cmd == "attach" {
		fwd = append(fwd, "--switch-prev-key", g.switchPrev, "--switch-next-key", g.switchNext)
	}
	if g.setTopicLen && cmd == "ls" {
		fwd = append(fwd, "--topic-len", strconv.Itoa(g.topicLen))
	}
	tty := cmd == "attach" || (cmd == "new" && !hasFlag(args, "-d") && !hasFlag(args, "--detached"))
	code, err := remote.Forward(remote.Options{
		Host:       g.host,
		RemoteBin:  g.remoteBin,
		TTY:        tty,
		ConfigFile: remote.ResolveConfig(g.sshConfig),
	}, fwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pism:", err)
		return 1
	}
	return code
}
