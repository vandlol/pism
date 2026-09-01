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

func containsSeq(set [][]byte, want []byte) bool {
	for _, s := range set {
		if bytes.Equal(s, want) {
			return true
		}
	}
	return false
}
