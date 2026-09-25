package bundle

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
	"sync"
)

var (
	ErrNotFound       = errors.New("document not found")
	ErrNoMatch        = errors.New("old string not found in document")
	ErrAmbiguousMatch = errors.New("old string matches more than once")
)

// Store holds the raw files of a knowledge bundle keyed by a slash-separated
// path relative to the bundle root, e.g. "metrics/revenue.md".
type Store interface {
	Read(ctx context.Context, path string) (string, error)
	Write(ctx context.Context, path string, content string) error
	// Edit replaces oldString with newString by exact match, like Claude
	// Code's Edit tool. oldString must occur exactly once unless replaceAll is
	// set, in which case every occurrence is replaced.
	Edit(ctx context.Context, path, oldString, newString string, replaceAll bool) error
	Delete(ctx context.Context, path string) error
	// List returns the markdown documents and subdirectories directly under
	// dir, like ls. Use "." for the bundle root.
	List(ctx context.Context, dir string) ([]Entry, error)
	// Tree returns the paths of markdown documents under dir, recursively,
	// like tree -L depth. Depth 1 is the documents directly under dir; -1
	// means no limit. Use "." for the bundle root.
	Tree(ctx context.Context, dir string, depth int) ([]string, error)
}

type Entry struct {
	Path  string
	IsDir bool
}

// DirStore is a Store backed by a local directory.
// Access is confined to the directory; paths escaping it are rejected.
type DirStore struct {
	root *os.Root
	// mu serializes writes so that concurrent edits don't lose updates.
	mu sync.Mutex
}

var _ Store = (*DirStore)(nil)

func OpenDir(dir string) (*DirStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &DirStore{root: root}, nil
}

func (r *DirStore) Close() error {
	return r.root.Close()
}

func (r *DirStore) Read(_ context.Context, name string) (string, error) {
	if err := validName(name); err != nil {
		return "", err
	}
	data, err := r.root.ReadFile(name)
	return string(data), notFound(name, err)
}

func (r *DirStore) Write(_ context.Context, name string, content string) error {
	if err := validName(name); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.root.MkdirAll(path.Dir(name), 0o755); err != nil {
		return err
	}
	return r.root.WriteFile(name, []byte(content), 0o644)
}

func (r *DirStore) Edit(ctx context.Context, name, oldString, newString string, replaceAll bool) error {
	if oldString == "" {
		return errors.New("old string must not be empty")
	}
	if oldString == newString {
		return errors.New("old string and new string must differ")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	content, err := r.Read(ctx, name)
	if err != nil {
		return err
	}
	switch n := strings.Count(content, oldString); {
	case n == 0:
		return fmt.Errorf("%w: %s", ErrNoMatch, name)
	case n > 1 && !replaceAll:
		return fmt.Errorf("%w: %d occurrences in %s", ErrAmbiguousMatch, n, name)
	}
	return r.root.WriteFile(name, []byte(strings.ReplaceAll(content, oldString, newString)), 0o644)
}

func (r *DirStore) Delete(_ context.Context, name string) error {
	if err := validName(name); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return notFound(name, r.root.Remove(name))
}

func (r *DirStore) List(_ context.Context, dir string) ([]Entry, error) {
	if !fs.ValidPath(dir) {
		return nil, fmt.Errorf("invalid directory path %q", dir)
	}
	des, err := fs.ReadDir(r.root.FS(), dir)
	if err != nil {
		return nil, notFound(dir, err)
	}
	var entries []Entry
	for _, d := range des {
		p := path.Join(dir, d.Name())
		switch {
		case d.IsDir() && !isHidden(d):
			entries = append(entries, Entry{Path: p, IsDir: true})
		case isDocument(d):
			entries = append(entries, Entry{Path: p})
		}
	}
	return entries, nil
}

func (r *DirStore) Tree(ctx context.Context, dir string, depth int) ([]string, error) {
	if !fs.ValidPath(dir) {
		return nil, fmt.Errorf("invalid directory path %q", dir)
	}
	var names []string
	err := fs.WalkDir(r.root.FS(), dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() && p != dir && (isHidden(d) || depth >= 0 && relDepth(dir, p) >= depth) {
			return fs.SkipDir
		}
		if isDocument(d) && (depth < 0 || relDepth(dir, p) <= depth) {
			names = append(names, p)
		}
		return nil
	})
	return names, notFound(dir, err)
}

// relDepth returns the number of path elements in p below dir.
func relDepth(dir, p string) int {
	if dir != "." {
		p = strings.TrimPrefix(p, dir+"/")
	}
	return strings.Count(p, "/") + 1
}

func isHidden(d fs.DirEntry) bool {
	return strings.HasPrefix(d.Name(), ".")
}

func isDocument(d fs.DirEntry) bool {
	return d.Type().IsRegular() && path.Ext(d.Name()) == ".md"
}

func validName(name string) error {
	if !fs.ValidPath(name) || name == "." {
		return fmt.Errorf("invalid document path %q", name)
	}
	return nil
}

func notFound(name string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return err
}
