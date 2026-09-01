package main

import "testing"

func TestSameHost(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"local", "", true}, // both mean local
		{"", "local", true}, // both mean local
		{"", "", true},      // both local
		{"local", "local", true},
		{"mac", "mac", true},
		{"mac", "srv", false},
		{"local", "mac", false},
		{"mac", "", false},
	}
	for _, c := range cases {
		if got := sameHost(c.a, c.b); got != c.want {
			t.Errorf("sameHost(%q,%q) = %v; want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestIndexOfHostAndPrefix(t *testing.T) {
	uni := []xtarget{
		{Host: "local", ID: "aaaa1111-2222"},
		{Host: "mac", ID: "bbbb3333-4444"},
		{Host: "srv", ID: "aaaa9999-8888"}, // same id-prefix as local, different host
	}
	// Prefix match, scoped to the right host.
	if i := indexOf(uni, xtarget{Host: "local", ID: "aaaa"}); i != 0 {
		t.Errorf("local aaaa -> %d, want 0", i)
	}
	if i := indexOf(uni, xtarget{Host: "srv", ID: "aaaa"}); i != 2 {
		t.Errorf("srv aaaa -> %d, want 2 (host-scoped, not local)", i)
	}
	// Empty host means local.
	if i := indexOf(uni, xtarget{Host: "", ID: "aaaa1111"}); i != 0 {
		t.Errorf("'' aaaa1111 -> %d, want 0", i)
	}
	if i := indexOf(uni, xtarget{Host: "mac", ID: "zzzz"}); i != -1 {
		t.Errorf("mac zzzz -> %d, want -1", i)
	}
}

func TestNextInUniverseWrapAndCrossHost(t *testing.T) {
	uni := []xtarget{
		{Host: "local", ID: "l1"},
		{Host: "local", ID: "l2"},
		{Host: "mac", ID: "m1"},
	}
	// next from last wraps to first.
	if got, ok := nextInUniverse(uni, xtarget{Host: "mac", ID: "m1"}, +1); !ok || got != uni[0] {
		t.Errorf("next(mac m1) = %+v ok=%v; want %+v", got, ok, uni[0])
	}
	// prev from first wraps to last (crossing local -> mac).
	if got, ok := nextInUniverse(uni, xtarget{Host: "local", ID: "l1"}, -1); !ok || got != uni[2] {
		t.Errorf("prev(local l1) = %+v ok=%v; want %+v", got, ok, uni[2])
	}
	// next within local crosses to the next local session.
	if got, ok := nextInUniverse(uni, xtarget{Host: "local", ID: "l1"}, +1); !ok || got != uni[1] {
		t.Errorf("next(local l1) = %+v ok=%v; want %+v", got, ok, uni[1])
	}
	// next from a session that's the only one and is current -> no switch.
	one := []xtarget{{Host: "local", ID: "l1"}}
	if _, ok := nextInUniverse(one, xtarget{Host: "local", ID: "l1"}, +1); ok {
		t.Error("single-session universe should report no switch target")
	}
	// unknown current lands on newest (index 0).
	if got, ok := nextInUniverse(uni, xtarget{Host: "srv", ID: "gone"}, +1); !ok || got != uni[0] {
		t.Errorf("next(unknown) = %+v ok=%v; want newest %+v", got, ok, uni[0])
	}
	// empty universe -> no switch.
	if _, ok := nextInUniverse(nil, xtarget{Host: "local", ID: "l1"}, +1); ok {
		t.Error("empty universe should report no switch target")
	}
}
