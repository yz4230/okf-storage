package okf

import (
	"errors"
	"maps"
	"slices"
	"testing"
)

func TestParseDocument(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		keys []string
		body string
	}{
		{
			name: "frontmatter and body",
			doc:  "---\ntype: Metric\ntitle: Revenue\n---\n# Definition\n",
			keys: []string{"title", "type"},
			body: "# Definition\n",
		},
		{
			name: "no trailing newline after closing delimiter",
			doc:  "---\ntype: Metric\n---",
			keys: []string{"type"},
			body: "",
		},
		{
			name: "CRLF line endings",
			doc:  "---\r\ntype: Metric\r\n---\r\nbody\r\n",
			keys: []string{"type"},
			body: "body\r\n",
		},
		{
			name: "empty frontmatter",
			doc:  "---\n---\nbody",
			body: "body",
		},
		{
			name: "no frontmatter",
			doc:  "# Index\n\n* [A](a.md)\n",
			body: "# Index\n\n* [A](a.md)\n",
		},
		{
			name: "empty document",
			doc:  "",
			body: "",
		},
		{
			name: "delimiter inside body is kept",
			doc:  "---\ntype: Metric\n---\nabove\n---\nbelow\n",
			keys: []string{"type"},
			body: "above\n---\nbelow\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := ParseDocument(tt.doc)
			if err != nil {
				t.Fatalf("ParseDocument() error = %v", err)
			}
			keys := slices.Sorted(maps.Keys(maps.Collect(d.Frontmatter.Iter())))
			if !slices.Equal(keys, tt.keys) {
				t.Errorf("keys = %q, want %q", keys, tt.keys)
			}
			if d.Body != tt.body {
				t.Errorf("Body = %q, want %q", d.Body, tt.body)
			}
		})
	}
}

func TestParseDocumentErrors(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want error
	}{
		{"unterminated", "---\ntype: Metric\n", ErrInvalidDocument},
		{"delimiter only", "---", ErrInvalidDocument},
		{"sequence", "---\n- a\n- b\n---\n", ErrInvalidFrontmatter},
		{"scalar", "---\nhello\n---\n", ErrInvalidFrontmatter},
		{"malformed yaml", "---\ntype: [\n---\n", ErrInvalidFrontmatter},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseDocument(tt.doc)
			if !errors.Is(err, tt.want) {
				t.Errorf("ParseDocument() error = %v, want %v", err, tt.want)
			}
		})
	}
}
