package bundle

import (
	"context"
	"log/slog"
	"sync"

	"github.com/yz4230/okf-storage/internal/okf"
)

type Bundle interface {
	Read(ctx context.Context, path string) (string, error)
	Search(ctx context.Context, filter map[string]any) ([]string, error)
	Write(ctx context.Context, path string, content string) error
	Edit(ctx context.Context, path, oldString, newString string, replaceAll bool) error
	Delete(ctx context.Context, path string) error
	List(ctx context.Context, dir string) ([]Entry, error)
	Tree(ctx context.Context, dir string, depth int) ([]string, error)
}

type bundle struct {
	store   Store
	catalog Catalog
	mu      sync.Mutex
}

var _ Bundle = (*bundle)(nil)

func NewBundle(store Store, catalog Catalog) Bundle {
	return &bundle{store: store, catalog: catalog}
}

// OpenBundle returns a Bundle whose catalog is populated with the frontmatter
// of every document already in store. Documents whose frontmatter fails to
// parse are skipped and logged.
func OpenBundle(ctx context.Context, store Store, catalog Catalog) (Bundle, error) {
	paths, err := store.Tree(ctx, ".", -1)
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		content, err := store.Read(ctx, path)
		if err != nil {
			return nil, err
		}
		doc, err := okf.ParseDocument(content)
		if err != nil {
			slog.Warn("skipping unparsable document", "path", path, "error", err)
			continue
		}
		if doc.Frontmatter != nil {
			if err := catalog.Put(ctx, path, doc.Frontmatter); err != nil {
				return nil, err
			}
		}
	}
	return NewBundle(store, catalog), nil
}

func (b *bundle) Read(ctx context.Context, path string) (string, error) {
	return b.store.Read(ctx, path)
}

func (b *bundle) Search(ctx context.Context, filter map[string]any) ([]string, error) {
	return b.catalog.Search(ctx, filter)
}

func (b *bundle) Write(ctx context.Context, path string, content string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.store.Write(ctx, path, content); err != nil {
		return err
	}
	doc, err := okf.ParseDocument(content)
	if err != nil {
		return err
	}
	if doc.Frontmatter != nil {
		if err := b.catalog.Put(ctx, path, doc.Frontmatter); err != nil {
			return err
		}
	}
	return nil
}

func (b *bundle) Edit(ctx context.Context, path, oldString, newString string, replaceAll bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.store.Edit(ctx, path, oldString, newString, replaceAll); err != nil {
		return err
	}

	latest, err := b.store.Read(ctx, path)
	if err != nil {
		return err
	}

	doc, err := okf.ParseDocument(latest)
	if err != nil {
		return err
	}
	if doc.Frontmatter != nil {
		if err := b.catalog.Put(ctx, path, doc.Frontmatter); err != nil {
			return err
		}
	} else {
		if err := b.catalog.Delete(ctx, path); err != nil {
			return err
		}
	}

	return nil
}

func (b *bundle) Delete(ctx context.Context, path string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.store.Delete(ctx, path); err != nil {
		return err
	}
	if err := b.catalog.Delete(ctx, path); err != nil {
		return err
	}
	return nil
}

func (b *bundle) List(ctx context.Context, dir string) ([]Entry, error) {
	return b.store.List(ctx, dir)
}

func (b *bundle) Tree(ctx context.Context, dir string, depth int) ([]string, error) {
	return b.store.Tree(ctx, dir, depth)
}
