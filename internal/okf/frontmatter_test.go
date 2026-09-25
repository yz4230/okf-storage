package okf

import (
	"errors"
	"testing"
)

func TestFrontmatterSetDelete(t *testing.T) {
	f := &Frontmatter{}
	f.Set("type", "Metric")
	f.Set("type", "Playbook")
	if v, _ := f.Get("type"); v != "Playbook" {
		t.Errorf("Get(type) = %v, want Playbook", v)
	}

	f.Delete("type")
	if _, ok := f.Get("type"); ok {
		t.Error("Get(type) after Delete: ok = true")
	}
}

func TestFrontmatterNil(t *testing.T) {
	var f *Frontmatter
	if _, ok := f.Get("type"); ok {
		t.Error("Get on nil: ok = true")
	}
	f.Delete("type")
	for range f.Iter() {
		t.Error("Iter on nil yielded a value")
	}
	if err := f.Validate(); !errors.Is(err, ErrInvalidFrontmatter) {
		t.Errorf("Validate on nil = %v, want %v", err, ErrInvalidFrontmatter)
	}
}

func TestFrontmatterValidate(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		ok   bool
	}{
		{"type only", "type: Metric", true},
		{"missing type", "title: Revenue", false},
		{"empty type", "type: ''", false},
		{"blank type", "type: '  '", false},
		{"null type", "type:", false},
		{"non-string type", "type: 1", false},
		{"unknown keys are allowed", "type: Metric\nx_custom: 1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := parseFrontmatter(tt.yaml)
			if err != nil {
				t.Fatalf("parseFrontmatter() error = %v", err)
			}
			err = f.Validate()
			if tt.ok && err != nil {
				t.Errorf("Validate() error = %v", err)
			}
			if !tt.ok && !errors.Is(err, ErrInvalidFrontmatter) {
				t.Errorf("Validate() error = %v, want %v", err, ErrInvalidFrontmatter)
			}
		})
	}
}
