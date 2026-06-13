package api

import "testing"

func TestNormalizeEngine(t *testing.T) {
	cases := map[string]string{
		"":           engineMySQL,
		"mysql":      engineMySQL,
		"mariadb":    engineMySQL, // unknown -> default
		"postgres":   enginePostgres,
		"postgresql": enginePostgres,
	}
	for in, want := range cases {
		if got := normalizeEngine(in); got != want {
			t.Errorf("normalizeEngine(%q) = %q, want %q", in, got, want)
		}
	}
}
