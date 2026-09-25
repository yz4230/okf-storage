package bundle

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/yz4230/okf-storage/internal/okf"
)

var ErrInvalidFilter = errors.New("invalid search filter")

// Catalog records concept frontmatter by path so concepts can be searched
// by metadata. Implementations must pass catalogtest.Run.
type Catalog interface {
	Put(ctx context.Context, path string, fm *okf.Frontmatter) error
	Delete(ctx context.Context, path string) error
	// Search returns the page of paths, sorted in byte order, of documents
	// whose frontmatter matches every key in filter. An empty filter matches
	// every document.
	//
	// Keys name top-level fields literally, even if they contain "." or "$".
	// Values must be strings, numbers or booleans (see ValidateFilter). A
	// field matches if it equals the value, or if it is a list with an element
	// that does. Equality is by type, except that numbers of any Go type are
	// compared by value, so the uint64 2 decoded from YAML equals the float64
	// 2 decoded from JSON while the string "2" equals neither.
	Search(ctx context.Context, filter map[string]any, page PageRequest) (Page, error)
}

// ValidateFilter reports whether every value in filter is a string, number or
// boolean, wrapping ErrInvalidFilter if not.
func ValidateFilter(filter map[string]any) error {
	for key, v := range filter {
		switch v.(type) {
		case string, bool:
		default:
			if _, ok := number(v); !ok {
				return fmt.Errorf("%w: %q must be a string, number or boolean, got %T", ErrInvalidFilter, key, v)
			}
		}
	}
	return nil
}

// MemCatalog is an in-memory Catalog. It keeps a reference to each
// frontmatter, so callers must not mutate one after putting it.
type MemCatalog struct {
	mu   sync.RWMutex
	docs map[string]*okf.Frontmatter
}

var _ Catalog = (*MemCatalog)(nil)

func NewMemCatalog() *MemCatalog {
	return &MemCatalog{docs: make(map[string]*okf.Frontmatter)}
}

func (x *MemCatalog) Put(_ context.Context, path string, fm *okf.Frontmatter) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.docs[path] = fm
	return nil
}

func (x *MemCatalog) Delete(_ context.Context, path string) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	delete(x.docs, path)
	return nil
}

func (x *MemCatalog) Search(_ context.Context, filter map[string]any, page PageRequest) (Page, error) {
	if err := ValidateFilter(filter); err != nil {
		return Page{}, err
	}
	x.mu.RLock()
	defer x.mu.RUnlock()
	var names []string
	for name, fm := range x.docs {
		if matches(fm, filter) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return paginate(names, page), nil
}

func matches(fm *okf.Frontmatter, filter map[string]any) bool {
	for key, want := range filter {
		got, ok := fm.Get(key)
		if !ok {
			return false
		}
		if list, isList := got.([]any); isList {
			if !slices.ContainsFunc(list, func(v any) bool { return equal(v, want) }) {
				return false
			}
		} else if !equal(got, want) {
			return false
		}
	}
	return true
}

// equal reports whether a frontmatter value equals a filter value, comparing
// numbers by value regardless of their Go type.
func equal(got, want any) bool {
	if w, ok := number(want); ok {
		g, ok := number(got)
		return ok && g == w
	}
	switch want.(type) {
	case string, bool:
		return got == want
	}
	return false
}

// number converts any Go numeric type to float64. Integers beyond 2^53 lose
// precision, which frontmatter values are not expected to reach.
func number(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}
