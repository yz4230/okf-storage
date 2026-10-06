package bundledir

import "testing"

func TestExpand(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	tests := map[string]string{
		"~":                     "/home/u",
		"~/":                    "/home/u",
		"~/.okf-storage/bundle": "/home/u/.okf-storage/bundle",
		"~user/x":               "~user/x",
		"./knowledge":           "./knowledge",
		"/data":                 "/data",
		"a/~/b":                 "a/~/b",
	}
	for in, want := range tests {
		got, err := Expand(in)
		if err != nil {
			t.Fatalf("Expand(%q) error = %v", in, err)
		}
		if got != want {
			t.Errorf("Expand(%q) = %q, want %q", in, got, want)
		}
	}
}
