package main

import (
	"bytes"
	"testing"
)

func TestParseSwitchNamedArrows(t *testing.T) {
	cases := map[string][]byte{
		"ctrl-left":  []byte("\x1b[1;5D"),
		"ctrl-right": []byte("\x1b[1;5C"),
		"alt-left":   []byte("\x1b[1;3D"),
		"f16":        []byte("\x1b[57379u"),
		"ctrl-o":     {0x0f},
		"^\\":        {0x1c},
	}
	for spec, want := range cases {
		if got := parseSwitch(spec); !bytes.Equal(got, want) {
			t.Errorf("parseSwitch(%q) = %q; want %q", spec, got, want)
		}
	}
}

func TestParseSwitchDisabled(t *testing.T) {
	for _, spec := range []string{"", "none", "off", "disable", "  "} {
		if got := parseSwitch(spec); got != nil {
			t.Errorf("parseSwitch(%q) = %q; want nil (disabled)", spec, got)
		}
	}
}

func TestParseSwitchUnknownDisables(t *testing.T) {
	// An unrecognized spec must disable (nil), never collide with a default.
	if got := parseSwitch("not-a-real-key"); got != nil {
		t.Errorf("parseSwitch(unknown) = %q; want nil", got)
	}
}

func TestParseDetachNamedArrow(t *testing.T) {
	// Named arrows are usable as a detach key too.
	if got := parseDetach("ctrl-left"); !bytes.Equal(got, []byte("\x1b[1;5D")) {
		t.Errorf("parseDetach(ctrl-left) = %q", got)
	}
}
