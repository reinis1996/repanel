package system

import "testing"

func TestPgEscape(t *testing.T) {
	cases := map[string]string{
		"simple":     "simple",
		"O'Brien":    "O''Brien",
		"''":         "''''",
		`back\slash`: `back\slash`, // backslash is literal with standard_conforming_strings
		"a'b'c":      "a''b''c",
	}
	for in, want := range cases {
		if got := pgEscape(in); got != want {
			t.Errorf("pgEscape(%q) = %q, want %q", in, got, want)
		}
	}
}
