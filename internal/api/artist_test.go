package api

import "testing"

func TestCleanArtist(t *testing.T) {
	cases := map[string]string{
		"&":                           "",
		" & ":                         "",
		"feat.":                       "",
		"Ellie Goulding":              "Ellie Goulding",
		"Ellie Goulding & Juice WRLD": "Ellie Goulding & Juice WRLD",
		"& Juice WRLD":                "Juice WRLD",
		"":                            "",
	}
	for in, want := range cases {
		if got := cleanArtist(in); got != want {
			t.Errorf("cleanArtist(%q) = %q, want %q", in, got, want)
		}
	}
}
