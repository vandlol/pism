package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/vandlol/pism/internal/client"
	"github.com/vandlol/pism/internal/manager"
	"github.com/vandlol/pism/internal/remote"
	"github.com/vandlol/pism/internal/session"
)

// localHost is the sentinel host name for sessions on this machine in the
// cross-host switch universe.
const localHost = "local"

// xtarget identifies one session in the cross-host universe. A Host of
// localHost means the session lives on this machine.
type xtarget struct {
	Host string
	ID   string
}

func (t xtarget) isLocal() bool { return t.Host == "" || t.Host == localHost }

func (t xtarget) label() string {
	if t.isLocal() {
		return shortID(t.ID)
	}
	return t.Host + ":" + shortID(t.ID)
}

// buildUniverse enumerates the live sessions across the local machine and every
// selected ssh-config host, ordered local-first then by ssh-config host order,
// newest-first within each host. Unreachable / pism-less hosts are skipped
// silently (they already got a note at attach start). It is rebuilt on each
// switch so newly-created and newly-dead sessions are reflected.
func buildUniverse(g globals, sel allHostsSel) []xtarget {
	var uni []xtarget

	// Local live sessions, newest-first.
	if rows, err := manager.Rows(g.topicLen); err == nil {
		for _, r := range rows {
			if r.Alive {
				uni = append(uni, xtarget{Host: localHost, ID: r.Meta.ID})
			}
		}
	}

	// Remote hosts.
	results, err := remote.GatherAll(remote.RunAllOptions{
		ConfigFile:     remote.ResolveConfig(g.sshConfig),
		RemoteBin:      g.remoteBin,
		Include:        sel.include,
		Exclude:        sel.exclude,
		Sub:            "ls",
		Args:           []string{"--porcelain"},
		ConnectTimeout: sel.timeout,
	})
	if err != nil {
		return uni
	}
	for _, res := range results {
		if res.Skipped || res.Err != nil {
			continue
		}
		for _, line := range strings.Split(string(res.Stdout), "\n") {
			f := strings.SplitN(strings.TrimSpace(line), "\t", 5)
			if len(f) < 5 || f[1] != "live" {
				continue
			}
			uni = append(uni, xtarget{Host: res.Host, ID: f[0]})
		}
	}
	return uni
}

// indexOf finds t in the universe, matching host and id (id by exact or prefix
// so a user-supplied prefix resolves). Returns -1 if absent.
func indexOf(uni []xtarget, t xtarget) int {
	for i, u := range uni {
		if !sameHost(u.Host, t.Host) {
			continue
		}
		if u.ID == t.ID || strings.HasPrefix(u.ID, t.ID) {
			return i
		}
	}
	return -1
}

func sameHost(a, b string) bool {
	an := a == "" || a == localHost
	bn := b == "" || b == localHost
	if an || bn {
		return an && bn
	}
	return a == b
}

// attachOne attaches the local terminal to a single target: a local holder
// directly, or a remote holder over the ssh proxy. It returns the attach
// outcome so the orchestrator can decide whether to switch.
func attachOne(g globals, t xtarget, keys client.Keys) (client.Outcome, error) {
	if t.isLocal() {
		m, err := session.Load(t.ID)
		if err != nil {
			return client.OutcomeExit, err
		}
		return client.Attach(m, keys)
	}
	opts := remote.Options{Host: t.Host, RemoteBin: g.remoteBin, ConfigFile: remote.ResolveConfig(g.sshConfig)}
	conn, err := remote.AttachProxy(opts, t.ID)
	if err != nil {
		return client.OutcomeExit, err
	}
	outcome, aerr := client.AttachStream(conn, t.label(), keys)
	_ = conn.Close()
	return outcome, aerr
}

// orchestrate runs the cross-host attach/switch loop starting at `start`. On a
// switch key it rebuilds the universe, finds the adjacent live session (across
// all hosts, wrapping), and attaches to it — transparently crossing hosts.
func orchestrate(g globals, start xtarget, sel allHostsSel) int {
	keys := client.Keys{
		Detach:     parseDetach(g.detachStr),
		SwitchPrev: parseSwitch(g.switchPrev),
		SwitchNext: parseSwitch(g.switchNext),
	}
	cur := start
	for {
		outcome, err := attachOne(g, cur, keys)
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
		next, ok := adjacentTarget(g, cur, dir, sel)
		if !ok {
			// Nothing else live anywhere; re-attach to the same session.
			continue
		}
		cur = next
		fmt.Fprintf(os.Stderr, "[switching to %s]\r\n", cur.label())
	}
}

// adjacentTarget rebuilds the universe and returns the target adjacent to cur
// (dir=+1 next, -1 prev, wrapping). ok is false when there are no live
// sessions to switch to. When cur is no longer present (it died), it lands on
// the newest session.
func adjacentTarget(g globals, cur xtarget, dir int, sel allHostsSel) (xtarget, bool) {
	return nextInUniverse(buildUniverse(g, sel), cur, dir)
}

// nextInUniverse is the pure adjacency step over an ordered universe: dir=+1
// next, -1 prev, wrapping. ok is false when there's nothing to switch to (empty
// universe, or the only live session is the current one).
func nextInUniverse(uni []xtarget, cur xtarget, dir int) (xtarget, bool) {
	if len(uni) == 0 {
		return xtarget{}, false
	}
	if len(uni) == 1 {
		if indexOf(uni, cur) == 0 {
			return xtarget{}, false
		}
		return uni[0], true
	}
	i := indexOf(uni, cur)
	if i == -1 {
		return uni[0], true
	}
	step := 1
	if dir < 0 {
		step = -1
	}
	n := len(uni)
	return uni[((i+step)%n+n)%n], true
}
