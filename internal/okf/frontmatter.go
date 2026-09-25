package okf

import (
	"fmt"
	"iter"
	"maps"
	"strings"

	"github.com/goccy/go-yaml"
)

type Frontmatter struct {
	fields map[string]any
}

func parseFrontmatter(data string) (*Frontmatter, error) {
	var fields map[string]any
	if err := yaml.Unmarshal([]byte(data), &fields); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidFrontmatter, err)
	}
	return &Frontmatter{fields: fields}, nil
}

func (f *Frontmatter) Get(key string) (any, bool) {
	if f == nil {
		return nil, false
	}
	value, ok := f.fields[key]
	return value, ok
}

func (f *Frontmatter) Set(key string, value any) {
	if f.fields == nil {
		f.fields = make(map[string]any)
	}
	f.fields[key] = value
}

func (f *Frontmatter) Delete(key string) {
	if f == nil {
		return
	}
	delete(f.fields, key)
}

// Iter yields fields in unspecified order.
func (f *Frontmatter) Iter() iter.Seq2[string, any] {
	if f == nil {
		return func(func(string, any) bool) {}
	}
	return maps.All(f.fields)
}

// Validate checks the frontmatter against the OKF conformance rules (§11):
// a non-empty `type` string is required.
func (f *Frontmatter) Validate() error {
	v, ok := f.Get("type")
	if !ok {
		return fmt.Errorf("%w: missing required key %q", ErrInvalidFrontmatter, "type")
	}
	if s, ok := v.(string); !ok || strings.TrimSpace(s) == "" {
		return fmt.Errorf("%w: %q must be a non-empty string", ErrInvalidFrontmatter, "type")
	}
	return nil
}
