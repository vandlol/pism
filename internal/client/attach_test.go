package client

import (
	"bytes"
	"testing"
)

func TestDetachVariantsKittyCSIu(t *testing.T) {
	// F16 bare form (what kitty emits) must also match the ";1" form that
	// wezterm/ghostty emit, and vice-versa.
	bare := []byte("\x1b[57379u")
	withMod := []byte("\x1b[57379;1u")

	for _, key := range [][]byte{bare, withMod} {
		vs := detachVariants(key)
		if !containsSeq(vs, bare) || !containsSeq(vs, withMod) {
			t.Fatalf("detachVariants(%q) = %q; want both bare and ;1 forms", key, vs)
		}
	}
}

func TestIndexAnyMatchesStreamedForm(t *testing.T) {
	keys := detachVariants([]byte("\x1b[57379u")) // config says f16 -> bare
	// terminal actually streams the ";1" form.
	stream := []byte("hello\x1b[57379;1uworld")
	i, n := indexAny(stream, keys)
	if i != 5 {
		t.Fatalf("index = %d; want 5", i)
	}
	if got := stream[i : i+n]; !bytes.Equal(got, []byte("\x1b[57379;1u")) {
		t.Fatalf("matched %q; want the ;1 form", got)
	}
}

func TestDetachVariantsNonCSIuUnchanged(t *testing.T) {
	k := []byte{0x1c} // Ctrl-\
	vs := detachVariants(k)
	if len(vs) != 1 || !bytes.Equal(vs[0], k) {
		t.Fatalf("single-byte key should have exactly one variant, got %q", vs)
	}
}

func TestInputProtoResetDisablesModifyOtherKeys(t *testing.T) {
	// The regression this guards: teardown must emit modifyOtherKeys-off, not
	// just the Kitty pop. Without \x1b[>4;0m the terminal stays wedged.
	if !bytes.Contains(inputProtoReset, []byte("\x1b[>4;0m")) {
		t.Fatalf("inputProtoReset %q missing modifyOtherKeys-off \\x1b[>4;0m", inputProtoReset)
	}
	if !bytes.Contains(inputProtoReset, []byte("\x1b[<u")) {
		t.Fatalf("inputProtoReset %q missing Kitty pop \\x1b[<u", inputProtoReset)
	}
}

func TestMatchCSIuVariants(t *testing.T) {
	code := []byte("57379") // F16
	cases := []struct {
		name   string
		chunk  string
		idx    int
		length int
	}{
		{"bare", "\x1b[57379u", 0, 8},
		{"no-mod ;1", "\x1b[57379;1u", 0, 10},
		{"modified ;2", "x\x1b[57379;2u", 1, 10},
		{"key-release ;1:3", "\x1b[57379;1:3u", 0, 12},
		{"embedded", "ab\x1b[57379ucd", 2, 8},
	}
	for _, c := range cases {
		i, n := matchCSIu([]byte(c.chunk), code)
		if i != c.idx || n != c.length {
			t.Errorf("%s: matchCSIu(%q) = (%d,%d); want (%d,%d)", c.name, c.chunk, i, n, c.idx, c.length)
		}
	}
}

func TestMatchCSIuNoFalsePrefix(t *testing.T) {
	// A longer keycode sharing our prefix must NOT match (57379 vs 573790).
	if i, _ := matchCSIu([]byte("\x1b[573790u"), []byte("57379")); i != -1 {
		t.Fatalf("matchCSIu matched a longer keycode sharing the prefix (i=%d)", i)
	}
	// A different keycode must not match.
	if i, _ := matchCSIu([]byte("\x1b[57376u"), []byte("57379")); i != -1 {
		t.Fatalf("matchCSIu matched a different keycode (i=%d)", i)
	}
}

func TestKeyMatcherRoutesCSIuVsExact(t *testing.T) {
	// F16 (CSI-u) matcher accepts a modified variant the exact-variant matcher
	// would miss.
	m := keyMatcher([]byte("\x1b[57379u"))
	if i, _ := m([]byte("\x1b[57379;5u")); i != 0 {
		t.Fatalf("CSI-u keyMatcher should match modified form; got %d", i)
	}
	// Ctrl-\ (single byte) matcher does an exact match.
	m2 := keyMatcher([]byte{0x1c})
	if i, _ := m2([]byte("ab\x1ccd")); i != 2 {
		t.Fatalf("byte keyMatcher exact match failed; got %d", i)
	}
	// ctrl-left arrow (\x1b[1;5D) is NOT CSI-u; must match exactly.
	m3 := keyMatcher([]byte("\x1b[1;5D"))
	if i, _ := m3([]byte("\x1b[1;5D")); i != 0 {
		t.Fatalf("arrow keyMatcher exact match failed; got %d", i)
	}
}

func containsSeq(set [][]byte, want []byte) bool {
	for _, s := range set {
		if bytes.Equal(s, want) {
			return true
		}
	}
	return false
}
