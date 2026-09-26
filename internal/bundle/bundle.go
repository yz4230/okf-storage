package bundle

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/yz4230/okf-storage/internal/okf"
)

// Bundle keeps the catalog derived from the store. It assumes it is the only
// writer to both, as when one process serves a DirStore with a MemCatalog
// (OpenDir enforces this), so a mutex held across each store change and the
// catalog update that follows keeps them in step. Readers take no lock and
// may briefly see the store ahead of the catalog.
type Bundle struct {
	store   Store
	catalog Catalog
	mu      sync.Mutex
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
		doc, err := load(ctx, store, path)
		if err != nil {
			return err
		}
		return index(ctx, catalog, path, doc)
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
		return catalog.Delete(ctx, path)
	})
}

// listPageSize is how many paths eachPage fetches at a time.
const listPageSize = 1000

// eachPage calls f for every path of every page that fetch returns.
func eachPage(fetch func(PageRequest) (Page, error), f func(path string) error) error {
	req := PageRequest{Limit: listPageSize}
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
	return b.store.Read(ctx, path)
}

func (b *Bundle) Search(ctx context.Context, filter map[string]any, page PageRequest) (Page, error) {
	return b.catalog.Search(ctx, filter, page)
}

// Write creates the document at path or replaces it whole with content, like
// Claude Code's Write tool; it never appends or merges. It reports whether
// the document was created rather than overwritten.
func (b *Bundle) Write(ctx context.Context, path string, content string) (created bool, err error) {
	doc, err := okf.ParseDocument(content)
	if err != nil {
		return false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	_, err = b.store.Read(ctx, path)
	created = errors.Is(err, ErrNotFound)
	if err != nil && !created {
		return false, err
	}
	if err := b.store.Write(ctx, path, content); err != nil {
		return false, err
	}
	return created, index(ctx, b.catalog, path, doc)
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

	b.mu.Lock()
	defer b.mu.Unlock()
	content, err := b.store.Read(ctx, path)
	if err != nil {
		return err
	}
	switch n := strings.Count(content, oldString); {
	case n == 0:
		return fmt.Errorf("%w: %s", ErrNoMatch, path)
	case n > 1 && !replaceAll:
		return fmt.Errorf("%w: %d occurrences in %s", ErrAmbiguousMatch, n, path)
	}
	content = strings.ReplaceAll(content, oldString, newString)
	doc, err := okf.ParseDocument(content)
	if err != nil {
		return err
	}
	if err := b.store.Write(ctx, path, content); err != nil {
		return err
	}
	return index(ctx, b.catalog, path, doc)
}

func (b *Bundle) Delete(ctx context.Context, path string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.store.Delete(ctx, path); err != nil {
		return err
	}
	return b.catalog.Delete(ctx, path)
}

// DeleteDir deletes every document under dir, recursively, and returns their
// paths. On failure it returns the paths deleted so far with the error.
func (b *Bundle) DeleteDir(ctx context.Context, dir string) ([]string, error) {
	if dir == "." {
		return nil, errors.New("refusing to delete the bundle root")
	}
	var deleted []string
	err := eachPage(func(req PageRequest) (Page, error) {
		page, err := b.store.Tree(ctx, dir, -1, req)
		// Deleting the last document of dir removes dir itself.
		if req.After != "" && errors.Is(err, ErrNotFound) {
			return Page{}, nil
		}
		return page, err
	}, func(path string) error {
		// A concurrent call may have deleted it since the listing.
		if err := b.Delete(ctx, path); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		deleted = append(deleted, path)
		return nil
	})
	return deleted, err
}

// Move renames the document at from to to. It fails with ErrExists if a
// document is already at to.
func (b *Bundle) Move(ctx context.Context, from, to string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.store.Move(ctx, from, to); err != nil {
		return err
	}
	doc, err := load(ctx, b.store, to)
	if err != nil {
		return err
	}
	if err := index(ctx, b.catalog, to, doc); err != nil {
		return err
	}
	return b.catalog.Delete(ctx, from)
}

func (b *Bundle) List(ctx context.Context, dir string) ([]Entry, error) {
	return b.store.List(ctx, dir)
}

func (b *Bundle) Tree(ctx context.Context, dir string, depth int, page PageRequest) (Page, error) {
	return b.store.Tree(ctx, dir, depth, page)
}

// load reads and parses the document at path. A missing document yields a nil
// doc, as does one whose frontmatter fails to parse (with a warning), so that
// neither gets a catalog entry.
func load(ctx context.Context, store Store, path string) (*okf.Document, error) {
	content, err := store.Read(ctx, path)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	doc, err := okf.ParseDocument(content)
	if err != nil {
		slog.Warn("leaving unparsable document out of catalog", "path", path, "error", err)
		return nil, nil
	}
	return doc, nil
}

// index makes the catalog entry for path reflect doc, dropping it when doc is
// nil or has no frontmatter.
func index(ctx context.Context, catalog Catalog, path string, doc *okf.Document) error {
	if doc == nil || doc.Frontmatter == nil {
		return catalog.Delete(ctx, path)
	}
	return catalog.Put(ctx, path, doc.Frontmatter)
}

// Dump writes every document in the bundle to w as a gzip-compressed tar
// archive, with paths relative to the bundle root. Documents deleted while
// dumping are left out.
func (b *Bundle) Dump(ctx context.Context, w io.Writer) error {
	zw := gzip.NewWriter(w)
	tw := tar.NewWriter(zw)
	now := time.Now()
	err := eachPage(func(req PageRequest) (Page, error) {
		return b.store.Tree(ctx, ".", -1, req)
	}, func(path string) error {
		content, err := b.store.Read(ctx, path)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		hdr := &tar.Header{Name: path, Mode: 0o644, Size: int64(len(content)), ModTime: now}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		_, err = io.WriteString(tw, content)
		return err
	})
	if err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return zw.Close()
}
