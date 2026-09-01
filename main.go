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
	"github.com/vandlol/pism/internal/holder"
	"github.com/vandlol/pism/internal/manager"
	"github.com/vandlol/pism/internal/remote"
	"github.com/vandlol/pism/internal/session"
	"github.com/vandlol/pism/internal/ui"
)

var version = "dev" // overridden at build time via -ldflags "-X main.version=..."

type globals struct {
	host      string
	remoteBin string
	pi        string
	detachStr string
	sshConfig string
	topicLen  int

	readyTimeout time.Duration

	setPi       bool
	setDetach   bool
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

	g := globals{pi: "pi", detachStr: "^\\", topicLen: 40, dist: "dist", readyTimeout: 30 * time.Second}
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
		return cmdConfig(args)
	case "update", "self-update":
		return cmdUpdate(args)
	case "install":
		// pism install <host> [flags]
		return cmdInstall(g, args)
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
	default:
		fmt.Fprintf(os.Stderr, "pism: unknown command %q\n", cmd)
		usage()
		return 2
	}
}

func runHolder(args []string) int {
	var id, cwd, pi string
	var extra []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--id":
			i++
			id = args[i]
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
	if err := holder.Run(holder.Config{ID: id, Cwd: cwd, PiCmd: pi, ExtraArgs: extra}); err != nil {
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
	if detached {
		fmt.Println(id)
		return 0
	}
	fmt.Fprintf(os.Stderr, "[attached %s — detach with %s]\n", id[:8], g.detachStr)
	return attachByID(g, id)
}

func cmdLs(g globals, _ []string) int {
	rows, err := manager.Rows(g.topicLen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pism ls:", err)
		return 1
	}
	ui.Render(os.Stdout, rows)
	return 0
}

func cmdAttach(g globals, args []string) int {
	if len(args) == 0 {
		// No id: resume the most recently created live session.
		m, err := manager.NewestLive()
		if err != nil {
			fmt.Fprintln(os.Stderr, "pism attach:", err)
			return 1
		}
		return attachByID(g, m.ID)
	}
	return attachByID(g, args[0])
}

func attachByID(g globals, idOrPrefix string) int {
	m, err := session.Load(idOrPrefix)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pism attach:", err)
		return 1
	}
	err = client.Attach(m, parseDetach(g.detachStr))
	if err != nil {
		if ee, ok := err.(*client.ExitError); ok {
			return ee.Code
		}
		fmt.Fprintln(os.Stderr, "pism attach:", err)
		return 1
	}
	return 0
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
