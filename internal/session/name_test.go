package session

import (
	"strings"
	"testing"
)

func TestGenerateNameShapeAndUniqueness(t *testing.T) {
	n := GenerateName(nil)
	if !ValidName(n) {
		t.Fatalf("generated name %q is not valid", n)
	}
	if !strings.Contains(n, "-") {
		t.Errorf("generated name %q should be adjective-noun", n)
	}
	// A fully-taken first-pass space still terminates via the numeric suffix.
	taken := map[string]bool{}
	for _, a := range nameAdjectives {
		for _, b := range nameNouns {
			taken[a+"-"+b] = true
		}
	}
	n2 := GenerateName(taken)
	if taken[n2] {
		t.Errorf("GenerateName returned a taken name %q", n2)
	}
	if !ValidName(n2) {
		t.Errorf("suffixed name %q is not valid", n2)
	}
}

func TestValidName(t *testing.T) {
	ok := []string{"calm-otter", "a", "web_1", "brave-falcon-2", "x0"}
	bad := []string{"", "Has-Caps", "with space", "-leading", "wöw", strings.Repeat("a", 32), "tab\ttab"}
	for _, s := range ok {
		if !ValidName(s) {
			t.Errorf("ValidName(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if ValidName(s) {
			t.Errorf("ValidName(%q) = true, want false", s)
		}
	}
}
