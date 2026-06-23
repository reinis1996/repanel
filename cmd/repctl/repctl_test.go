package main

import (
	"testing"
	"time"
)

func TestSplitFlag(t *testing.T) {
	cases := []struct {
		in     string
		name   string
		val    string
		hasVal bool
	}{
		{"--url=https://x", "--url", "https://x", true},
		{"--insecure", "--insecure", "", false},
		{"--token=", "--token", "", true},
		{"-h", "-h", "", false},
	}
	for _, c := range cases {
		n, v, has := splitFlag(c.in)
		if n != c.name || v != c.val || has != c.hasVal {
			t.Errorf("splitFlag(%q) = (%q,%q,%v), want (%q,%q,%v)", c.in, n, v, has, c.name, c.val, c.hasVal)
		}
	}
}

func TestMergeEnv(t *testing.T) {
	t.Setenv("REPANEL_URL", "https://env")
	t.Setenv("REPANEL_TOKEN", "rpat_env")
	t.Setenv("REPANEL_INSECURE", "1")

	got := Config{URL: "https://file", Token: "rpat_file"}.mergeEnv()
	if got.URL != "https://env" || got.Token != "rpat_env" || !got.Insecure {
		t.Fatalf("mergeEnv did not overlay env vars: %+v", got)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		0:          "0 B",
		512:        "512 B",
		1024:       "1.0 KB",
		1536:       "1.5 KB",
		1048576:    "1.0 MB",
		1073741824: "1.0 GB",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestPtime(t *testing.T) {
	if ptime(nil) != "never" {
		t.Error("nil time should render as never")
	}
	var zero time.Time
	if ptime(&zero) != "never" {
		t.Error("zero time should render as never")
	}
	tm := time.Date(2026, 6, 13, 9, 30, 0, 0, time.Local)
	if got := ptime(&tm); got != "2026-06-13 09:30" {
		t.Errorf("ptime = %q", got)
	}
}
