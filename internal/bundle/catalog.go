package bundle

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/yz4230/okf-storage/internal/okf"
)

// Catalog records concept frontmatter by path so concepts can be searched
// by metadata.
type Catalog interface {
	Put(ctx context.Context, path string, fm *okf.Frontmatter) error
	Delete(ctx context.Context, path string) error
	// Search returns the sorted paths of documents whose frontmatter matches
	// every key in filter. A field matches if it equals the value, or if it is
	// a list containing the value. An empty filter matches every document.
	Search(ctx context.Context, filter map[string]any) ([]string, error)
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

func (x *MemCatalog) Search(_ context.Context, filter map[string]any) ([]string, error) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	var names []string
	for name, fm := range x.docs {
		if matches(fm, filter) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names, nil
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

// equal compares scalars by their printed form so that numbers decoded from
// YAML (int64, uint64) match those decoded from JSON (float64).
func equal(a, b any) bool {
	return fmt.Sprint(a) == fmt.Sprint(b)
}
