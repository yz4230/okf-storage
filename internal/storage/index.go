package storage

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/yz4230/okf-storage/internal/okf"
)

// DocumentIndex indexes document frontmatter by document path for search.
type DocumentIndex interface {
	Put(ctx context.Context, name string, fm *okf.Frontmatter) error
	Delete(ctx context.Context, name string) error
	// Search returns the sorted paths of documents whose frontmatter matches
	// every key in filter. A field matches if it equals the value, or if it is
	// a list containing the value. An empty filter matches every document.
	Search(ctx context.Context, filter map[string]any) ([]string, error)
}

// MemoryIndex is an in-memory DocumentIndex. It keeps a reference to each
// frontmatter, so callers must not mutate one after putting it.
type MemoryIndex struct {
	mu   sync.RWMutex
	docs map[string]*okf.Frontmatter
}

var _ DocumentIndex = (*MemoryIndex)(nil)

func NewMemoryIndex() *MemoryIndex {
	return &MemoryIndex{docs: make(map[string]*okf.Frontmatter)}
}

func (x *MemoryIndex) Put(_ context.Context, name string, fm *okf.Frontmatter) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.docs[name] = fm
	return nil
}

func (x *MemoryIndex) Delete(_ context.Context, name string) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	delete(x.docs, name)
	return nil
}

func (x *MemoryIndex) Search(_ context.Context, filter map[string]any) ([]string, error) {
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
