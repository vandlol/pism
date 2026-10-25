package remote

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestListConfigHosts(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config")
	content := `# comment
Host alpha
    HostName 10.0.0.1
    User root

Host beta gamma
    HostName 10.0.0.2

Host *.example.com
    User admin

Host prod-*
    User deploy

Host=delta
    HostName 10.0.0.3

Host alpha
    # duplicate should be ignored
`
	if err := os.WriteFile(cfg, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ListConfigHosts(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "beta", "gamma", "delta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListConfigHosts = %v; want %v", got, want)
	}
}

func TestSplitConfigLine(t *testing.T) {
	cases := map[string][2]string{
		"Host alpha":         {"Host", "alpha"},
		"Host=delta":         {"Host", "delta"},
		"Host   =  epsilon":  {"Host", "epsilon"},
		"HostName\t10.0.0.1": {"HostName", "10.0.0.1"},
		"Host":               {"Host", ""},
	}
	for in, want := range cases {
		k, r := splitConfigLine(in)
		if k != want[0] || r != want[1] {
			t.Errorf("splitConfigLine(%q) = (%q,%q); want (%q,%q)", in, k, r, want[0], want[1])
		}
	}
}

func TestMatchesAny(t *testing.T) {
	if !MatchesAny("prod-web", []string{"prod-*"}) {
		t.Error("prod-web should match prod-*")
	}
	if !MatchesAny("alpha", []string{"beta", "alpha"}) {
		t.Error("alpha should match exact")
	}
	if MatchesAny("alpha", []string{"beta", "prod-*"}) {
		t.Error("alpha should not match")
	}
	if MatchesAny("x", nil) {
		t.Error("empty patterns never match")
	}
}

func TestKeepaliveArgs(t *testing.T) {
	// The terminal-reset fix only fires once ssh actually exits; without
	// keepalives a half-open link keeps ssh blocked for minutes.
	if got := keepaliveArgs(0); got != nil {
		t.Fatalf("keepaliveArgs(0) = %v; want nil (leave ssh's own config alone)", got)
	}
	if got := keepaliveArgs(-1); got != nil {
		t.Fatalf("keepaliveArgs(-1) = %v; want nil", got)
	}
	got := keepaliveArgs(DefaultKeepalive)
	want := []string{
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=4",
		"-o", "TCPKeepAlive=yes",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("keepaliveArgs(%d) = %v; want %v", DefaultKeepalive, got, want)
	}
}

func TestRetryDelaySchedule(t *testing.T) {
	// 1s, 2s, 4s, 8s, 16s, then the 20s cap — identity jitter for determinism.
	r := Retry{Attempts: 7, Base: time.Second, Max: 20 * time.Second,
		Jitter: func(d time.Duration) time.Duration { return d }}
	want := []time.Duration{1, 2, 4, 8, 16, 20, 20}
	for i, w := range want {
		if got := r.Delay(i + 1); got != w*time.Second {
			t.Errorf("Delay(%d) = %s; want %s", i+1, got, w*time.Second)
		}
	}
}

func TestRetryDelayDefaultsAndJitter(t *testing.T) {
	// No Retry settings at all must still yield a sane, bounded wait, and the
	// default jitter must stay inside [d/2, d] so the first retry is snappy.
	r := Retry{}
	for attempt := 1; attempt <= 12; attempt++ {
		d := r.Delay(attempt)
		if d <= 0 || d > DefaultMaxBackoff {
			t.Fatalf("Delay(%d) = %s; want (0, %s]", attempt, d, DefaultMaxBackoff)
		}
		base := time.Second
		for i := 1; i < attempt && base < DefaultMaxBackoff; i++ {
			base *= 2
		}
		if base > DefaultMaxBackoff {
			base = DefaultMaxBackoff
		}
		if d < base/2 || d > base {
			t.Fatalf("Delay(%d) = %s; want within [%s, %s]", attempt, d, base/2, base)
		}
	}
}

func TestRetryTries(t *testing.T) {
	cases := []struct{ attempts, want int }{{0, 1}, {-2, 1}, {1, 1}, {5, 5}}
	for _, c := range cases {
		if got := (Retry{Attempts: c.attempts}).tries(); got != c.want {
			t.Errorf("tries(%d) = %d; want %d", c.attempts, got, c.want)
		}
	}
}

// fakeSSH writes an executable stub that fails with failCode for the first
// failCount invocations (then exits 0) and logs each invocation.
func fakeSSH(t *testing.T, failCount, failCode int) (bin, logPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stub")
	}
	dir := t.TempDir()
	bin = filepath.Join(dir, "ssh")
	logPath = filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\necho call >> " + logPath + "\n" +
		"n=$(wc -l < " + logPath + ")\n" +
		"if [ \"$n\" -le " + strconv.Itoa(failCount) + " ]; then exit " + strconv.Itoa(failCode) + "; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, logPath
}

func calls(t *testing.T, logPath string) int {
	t.Helper()
	b, err := os.ReadFile(logPath)
	if err != nil {
		return 0
	}
	return len(strings.Split(strings.TrimSpace(string(b)), "\n"))
}

func TestForwardTUIRetriesLinkFailures(t *testing.T) {
	bin, logPath := fakeSSH(t, 2, FailureCode) // two drops, then success
	var events []AttemptEvent
	code, err := ForwardTUI(
		Options{Host: "srv", SSHBin: bin},
		[]string{"attach", "3f9a"},
		Retry{Attempts: 5, Base: time.Millisecond, Max: 2 * time.Millisecond},
		func(e AttemptEvent) { events = append(events, e) },
	)
	if err != nil || code != 0 {
		t.Fatalf("ForwardTUI = (%d, %v); want (0, nil)", code, err)
	}
	if got := calls(t, logPath); got != 3 {
		t.Fatalf("%d ssh invocations; want 3", got)
	}
	if len(events) != 2 || events[0].Attempt != 1 || events[0].Final {
		t.Fatalf("events = %+v; want two retry notifications", events)
	}
}

func TestForwardTUIGivesUpAfterBudget(t *testing.T) {
	bin, logPath := fakeSSH(t, 99, FailureCode) // never succeeds
	var events []AttemptEvent
	code, err := ForwardTUI(
		Options{Host: "srv", SSHBin: bin},
		[]string{"attach"},
		Retry{Attempts: 3, Base: time.Millisecond, Max: time.Millisecond},
		func(e AttemptEvent) { events = append(events, e) },
	)
	if err != nil || code != FailureCode {
		t.Fatalf("ForwardTUI = (%d, %v); want (%d, nil)", code, err, FailureCode)
	}
	if got := calls(t, logPath); got != 3 {
		t.Fatalf("%d ssh invocations; want 3", got)
	}
	if len(events) != 2 {
		t.Fatalf("events = %+v; want 2 retries (the last attempt is final, not retried)", events)
	}
}

func TestForwardTUIRetriesOnlySSHFailures(t *testing.T) {
	// A real remote error (bad flag, remote exit 1, auth denial that ssh reports
	// as a command failure) must not be retried: retrying hides it.
	bin, logPath := fakeSSH(t, 99, 1)
	code, err := ForwardTUI(
		Options{Host: "srv", SSHBin: bin},
		[]string{"attach"},
		Retry{Attempts: 5, Base: time.Millisecond, Max: time.Millisecond},
		nil,
	)
	if err != nil || code != 1 {
		t.Fatalf("ForwardTUI = (%d, %v); want (1, nil)", code, err)
	}
	if got := calls(t, logPath); got != 1 {
		t.Fatalf("%d ssh invocations; want exactly 1", got)
	}
}
