package main

import (
	"testing"
	"time"
)

func TestParsePorcelain(t *testing.T) {
	out := "" +
		"ee1329fc-f482-4e62-8fca-0e6a275025c4\tlive\t120\t~/proj\tdesign a caching layer\n" +
		"53a3728e-5d2f-4358-9dc5-4917c80e27f3\tdead\t7200\t~\tfix the flaky auth test\n" +
		"\n" // trailing blank line ignored

	rows := parsePorcelain("srv", []byte(out), 40)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	r := rows[0]
	if r.Host != "srv" {
		t.Errorf("host = %q, want srv", r.Host)
	}
	if r.ID != "ee1329fc-f482-4e62-8fca-0e6a275025c4" {
		t.Errorf("id = %q (should be the full id for attach)", r.ID)
	}
	if r.State != "live" {
		t.Errorf("state = %q, want live", r.State)
	}
	if r.Age != 120*time.Second {
		t.Errorf("age = %v, want 2m", r.Age)
	}
	if r.Dir != "~/proj" {
		t.Errorf("dir = %q, want ~/proj", r.Dir)
	}
	if r.Topic != "design a caching layer" {
		t.Errorf("topic = %q", r.Topic)
	}
}

func TestParsePorcelainSkipsMalformed(t *testing.T) {
	// Lines with too few fields are dropped, not panicking.
	out := "only\ttwo\nid\tlive\t5\t~\ttopic here\n"
	rows := parsePorcelain("h", []byte(out), 40)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1 (malformed line dropped)", len(rows))
	}
}

func TestParsePorcelainTruncatesTopic(t *testing.T) {
	long := "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz" // 51 runes
	out := "id\tlive\t1\t~\t" + long + "\n"
	rows := parsePorcelain("h", []byte(out), 10)
	got := rows[0].Topic
	if []rune(got)[10] != '…' || len([]rune(got)) != 11 {
		t.Errorf("topic = %q; want 10 runes + ellipsis", got)
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("hello", 10); got != "hello" {
		t.Errorf("short string changed: %q", got)
	}
	if got := truncateRunes("hello world", 5); got != "hello…" {
		t.Errorf("truncateRunes = %q, want hello…", got)
	}
	if got := truncateRunes("héllo wörld", 4); got != "héll…" {
		t.Errorf("multibyte truncateRunes = %q", got)
	}
}
