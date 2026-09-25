package bundle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/yz4230/okf-storage/internal/okf"
)

// maxEditAttempts bounds how often Edit retries after a concurrent write.
const maxEditAttempts = 3

// Bundle keeps the catalog derived from the store without holding a lock, so
// that several processes may share one store and catalog. The store is the
// source of truth; after changing it, a writer syncs the catalog entry and
// then checks the store is unchanged, re-deriving the entry if it is not.
// Whichever writer puts last therefore puts what the store holds. An entry can
// only go stale if a process dies between the two steps, which Reindex
// repairs.
type Bundle struct {
	store   Store
	catalog Catalog
}

func NewBundle(store Store, catalog Catalog) *Bundle {
	return &Bundle{store: store, catalog: catalog}
}

// Reindex makes catalog match the frontmatter of every document in store,
// adding missing entries and dropping those of documents that no longer
// exist. Run it to populate a catalog that does not persist on its own, such
// as a MemCatalog, or to repair one after a crash. Documents whose
// frontmatter fails to parse are left out and logged.
func Reindex(ctx context.Context, store Store, catalog Catalog) error {
	seen := make(map[string]bool)
	err := eachPage(func(req PageRequest) (Page, error) {
		return store.Tree(ctx, ".", -1, req)
	}, func(path string) error {
		seen[path] = true
		doc, ver, err := load(ctx, store, path)
		if err != nil {
			return err
		}
		return syncEntry(ctx, store, catalog, path, doc, ver)
	})
	if err != nil {
		return err
	}
	return eachPage(func(req PageRequest) (Page, error) {
		return catalog.Search(ctx, nil, req)
	}, func(path string) error {
		if seen[path] {
			return nil
		}
		return syncEntry(ctx, store, catalog, path, nil, "")
	})
}

// reindexPageSize is how many paths Reindex fetches at a time.
const reindexPageSize = 1000

// eachPage calls f for every path of every page that fetch returns.
func eachPage(fetch func(PageRequest) (Page, error), f func(path string) error) error {
	req := PageRequest{Limit: reindexPageSize}
	for {
		page, err := fetch(req)
		if err != nil {
			return err
		}
		for _, path := range page.Paths {
			if err := f(path); err != nil {
				return err
			}
		}
		if page.Next == "" {
			return nil
		}
		req.After = page.Next
	}
}

func (b *Bundle) Read(ctx context.Context, path string) (string, error) {
	content, _, err := b.store.Read(ctx, path)
	return content, err
}

func (b *Bundle) Search(ctx context.Context, filter map[string]any, page PageRequest) (Page, error) {
	return b.catalog.Search(ctx, filter, page)
}

func (b *Bundle) Write(ctx context.Context, path string, content string) error {
	doc, err := okf.ParseDocument(content)
	if err != nil {
		return err
	}
	ver, err := b.store.Write(ctx, path, content, "")
	if err != nil {
		return err
	}
	return syncEntry(ctx, b.store, b.catalog, path, doc, ver)
}

// Edit replaces oldString with newString by exact match, like Claude Code's
// Edit tool. oldString must occur exactly once unless replaceAll is set, in
// which case every occurrence is replaced.
func (b *Bundle) Edit(ctx context.Context, path, oldString, newString string, replaceAll bool) error {
	if oldString == "" {
		return errors.New("old string must not be empty")
	}
	if oldString == newString {
		return errors.New("old string and new string must differ")
	}

	var (
		doc *okf.Document
		ver Version
	)
	for attempt := 1; ; attempt++ {
		content, cur, err := b.store.Read(ctx, path)
		if err != nil {
			return err
		}
		switch n := strings.Count(content, oldString); {
		case n == 0:
			return fmt.Errorf("%w: %s", ErrNoMatch, path)
		case n > 1 && !replaceAll:
			return fmt.Errorf("%w: %d occurrences in %s", ErrAmbiguousMatch, n, path)
		}
		latest := strings.ReplaceAll(content, oldString, newString)
		if doc, err = okf.ParseDocument(latest); err != nil {
			return err
		}
		ver, err = b.store.Write(ctx, path, latest, cur)
		if err == nil {
			break
		}
		// Another writer changed the document between Read and Write; retry
		// against its content so the edit neither loses nor clobbers theirs.
		if !errors.Is(err, ErrConflict) || attempt == maxEditAttempts {
			return err
		}
	}

	return syncEntry(ctx, b.store, b.catalog, path, doc, ver)
}

func (b *Bundle) Delete(ctx context.Context, path string) error {
	if err := b.store.Delete(ctx, path); err != nil {
		return err
	}
	return syncEntry(ctx, b.store, b.catalog, path, nil, "")
}

func (b *Bundle) List(ctx context.Context, dir string) ([]Entry, error) {
	return b.store.List(ctx, dir)
}

func (b *Bundle) Tree(ctx context.Context, dir string, depth int, page PageRequest) (Page, error) {
	return b.store.Tree(ctx, dir, depth, page)
}

// syncEntry makes the catalog entry for path reflect the store, starting from
// the state the caller last saw: doc at version ver, or a nil doc and empty
// version if the document does not exist. Since another writer may change the
// document meanwhile and put its own entry first, syncEntry checks the store
// after every put and re-derives the entry until the store stops moving. Each
// extra round follows another write to path, so it ends once writes do.
func syncEntry(ctx context.Context, store Store, catalog Catalog, path string, doc *okf.Document, ver Version) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := index(ctx, catalog, path, doc); err != nil {
			return err
		}
		cur, err := store.Stat(ctx, path)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if cur == ver {
			return nil
		}
		if doc, ver, err = load(ctx, store, path); err != nil {
			return err
		}
	}
}

// load reads and parses the document at path. A missing document yields a nil
// doc and empty version, as does one whose frontmatter fails to parse (with a
// warning), so that neither gets a catalog entry.
func load(ctx context.Context, store Store, path string) (*okf.Document, Version, error) {
	content, ver, err := store.Read(ctx, path)
	if errors.Is(err, ErrNotFound) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	doc, err := okf.ParseDocument(content)
	if err != nil {
		slog.Warn("leaving unparsable document out of catalog", "path", path, "error", err)
		return nil, ver, nil
	}
	return doc, ver, nil
}

// index makes the catalog entry for path reflect doc, dropping it when doc is
// nil or has no frontmatter.
func index(ctx context.Context, catalog Catalog, path string, doc *okf.Document) error {
	if doc == nil || doc.Frontmatter == nil {
		return catalog.Delete(ctx, path)
	}
	return catalog.Put(ctx, path, doc.Frontmatter)
}
