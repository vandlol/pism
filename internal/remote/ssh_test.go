package remote

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
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
