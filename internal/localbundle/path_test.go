package localbundle

import (
	"errors"
	"testing"
)

func TestCleanPath(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"a.md", "a.md"},
		{"sub/a.md", "sub/a.md"},
		{"./sub//a.md", "sub/a.md"},
		{"sub/", "sub"},
		{"sub/.", "sub"},
		{"", "."},
		{".", "."},
		{"/", "."},
		{"//", "."},
		{"/a.md", "a.md"},
		{"/sub/", "sub"},
		{"/../a.md", "a.md"},
		{"..", ".."},
		{"../a.md", "../a.md"},
		{"sub/../a.md", "a.md"},
	}
	for _, tt := range tests {
		got, err := cleanPath(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("cleanPath(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}

	for _, p := range []string{".git", ".hidden.md", "a/.x.md", ".git/x.md", "a/../.git/x.md", "./.git", "/.git/x.md", "/a/.x.md", "..x"} {
		if _, err := cleanPath(p); !errors.Is(err, ErrHiddenPath) {
			t.Errorf("cleanPath(%q) error = %v, want ErrHiddenPath", p, err)
		}
	}
}

func TestIsHidden(t *testing.T) {
	for name, want := range map[string]bool{
		".":         false,
		"..":        false,
		"a.md":      false,
		"":          false,
		".git":      true,
		".x.md":     true,
		"...":       true,
		".okf.lock": true,
	} {
		if got := isHidden(name); got != want {
			t.Errorf("isHidden(%q) = %v, want %v", name, got, want)
		}
	}
}
