package bundle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/yz4230/okf-storage/internal/okf"
)

type Bundle interface {
	Read(ctx context.Context, path string) (string, error)
	Search(ctx context.Context, filter map[string]any) ([]string, error)
	Write(ctx context.Context, path string, content string) error
	// Edit replaces oldString with newString by exact match, like Claude
	// Code's Edit tool. oldString must occur exactly once unless replaceAll is
	// set, in which case every occurrence is replaced.
	Edit(ctx context.Context, path, oldString, newString string, replaceAll bool) error
	Delete(ctx context.Context, path string) error
	List(ctx context.Context, dir string) ([]Entry, error)
	Tree(ctx context.Context, dir string, depth int) ([]string, error)
}

// maxEditAttempts bounds how often Edit retries after a concurrent write.
const maxEditAttempts = 3

type bundle struct {
	store   Store
	catalog Catalog
	mu      sync.Mutex
}

var _ Bundle = (*bundle)(nil)

func NewBundle(store Store, catalog Catalog) Bundle {
	return &bundle{store: store, catalog: catalog}
}

// Reindex populates catalog with the frontmatter of every document in store.
// It is needed when catalog does not persist on its own, such as a
// MemCatalog. Documents whose frontmatter fails to parse are skipped and
// logged.
func Reindex(ctx context.Context, store Store, catalog Catalog) error {
	paths, err := store.Tree(ctx, ".", -1)
	if err != nil {
		return err
	}
	for _, path := range paths {
		content, _, err := store.Read(ctx, path)
		if err != nil {
			return err
		}
		doc, err := okf.ParseDocument(content)
		if err != nil {
			slog.Warn("skipping unparsable document", "path", path, "error", err)
			continue
		}
		if doc.Frontmatter != nil {
			if err := catalog.Put(ctx, path, doc.Frontmatter); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *bundle) Read(ctx context.Context, path string) (string, error) {
	content, _, err := b.store.Read(ctx, path)
	return content, err
}

func (b *bundle) Search(ctx context.Context, filter map[string]any) ([]string, error) {
	return b.catalog.Search(ctx, filter)
}

func (b *bundle) Write(ctx context.Context, path string, content string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, err := b.store.Write(ctx, path, content, ""); err != nil {
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

	if oldString == "" {
		return errors.New("old string must not be empty")
	}
	if oldString == newString {
		return errors.New("old string and new string must differ")
	}

	var latest string
	for attempt := 1; ; attempt++ {
		content, ver, err := b.store.Read(ctx, path)
		if err != nil {
			return err
		}
		switch n := strings.Count(content, oldString); {
		case n == 0:
			return fmt.Errorf("%w: %s", ErrNoMatch, path)
		case n > 1 && !replaceAll:
			return fmt.Errorf("%w: %d occurrences in %s", ErrAmbiguousMatch, n, path)
		}
		latest = strings.ReplaceAll(content, oldString, newString)
		_, err = b.store.Write(ctx, path, latest, ver)
		if err == nil {
			break
		}
		// Another writer changed the document between Read and Write; retry
		// against its content so the edit neither loses nor clobbers theirs.
		if !errors.Is(err, ErrConflict) || attempt == maxEditAttempts {
			return err
		}
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
