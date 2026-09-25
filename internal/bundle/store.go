package bundle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

var (
	ErrNotFound       = errors.New("document not found")
	ErrNoMatch        = errors.New("old string not found in document")
	ErrAmbiguousMatch = errors.New("old string matches more than once")
	ErrConflict       = errors.New("document was modified concurrently")
	ErrExists         = errors.New("document already exists")
)

// Version identifies the content of a document at the time it was read, like
// an HTTP ETag. It is opaque to callers and only compared for equality.
type Version string

// Store holds the raw files of a knowledge bundle keyed by a slash-separated
// path relative to the bundle root, e.g. "metrics/revenue.md".
type Store interface {
	Read(ctx context.Context, path string) (string, Version, error)
	// Stat returns the current version of path without its content, like an
	// HTTP HEAD.
	Stat(ctx context.Context, path string) (Version, error)
	// Write stores content at path and returns its new version. If ifMatch is
	// non-empty, the write only succeeds when the stored document still has
	// that version; otherwise it fails with ErrConflict.
	Write(ctx context.Context, path string, content string, ifMatch Version) (Version, error)
	// Delete removes the document at path. A directory left empty goes with
	// it, as in an object store where directories are only key prefixes.
	Delete(ctx context.Context, path string) error
	// Move renames the document at from to to, keeping its content. It fails
	// with ErrExists rather than overwrite a document already at to.
	Move(ctx context.Context, from, to string) error
	// List returns the markdown documents and subdirectories directly under
	// dir, like ls. Use "." for the bundle root.
	List(ctx context.Context, dir string) ([]Entry, error)
	// Tree returns the page of paths, sorted in byte order, of markdown
	// documents under dir, recursively, like tree -L depth. Depth 1 is the
	// documents directly under dir; -1 means no limit. Use "." for the bundle
	// root.
	Tree(ctx context.Context, dir string, depth int, page PageRequest) (Page, error)
}

type Entry struct {
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir,omitempty"`
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

func (r *DirStore) Read(_ context.Context, path string) (string, Version, error) {
	if err := validPath(path); err != nil {
		return "", "", err
	}
	data, err := r.root.ReadFile(path)
	if err != nil {
		return "", "", notFound(path, err)
	}
	return string(data), versionOf(data), nil
}

func (r *DirStore) Stat(ctx context.Context, path string) (Version, error) {
	_, ver, err := r.Read(ctx, path)
	return ver, err
}

func (r *DirStore) Write(_ context.Context, path string, content string, ifMatch Version) (Version, error) {
	if err := validPath(path); err != nil {
		return "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if ifMatch != "" {
		cur, err := r.root.ReadFile(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		if err != nil || versionOf(cur) != ifMatch {
			return "", fmt.Errorf("%w: %s", ErrConflict, path)
		}
	}
	if err := r.root.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	data := []byte(content)
	if err := r.root.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return versionOf(data), nil
}

func (r *DirStore) Delete(_ context.Context, path string) error {
	if err := validPath(path); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.root.Remove(path); err != nil {
		return notFound(path, err)
	}
	r.removeEmptyParents(path)
	return nil
}

// removeEmptyParents removes the directories above path that are now empty,
// stopping at the first that is not.
func (r *DirStore) removeEmptyParents(p string) {
	for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
		if r.root.Remove(dir) != nil {
			return
		}
	}
}

func (r *DirStore) Move(_ context.Context, from, to string) error {
	if err := validPath(from); err != nil {
		return err
	}
	if err := validPath(to); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.root.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	// Link fails if to exists, unlike Rename, so a document written there by
	// another process is never clobbered.
	if err := r.root.Link(from, to); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%w: %s", ErrExists, to)
		}
		return notFound(from, err)
	}
	if err := r.root.Remove(from); err != nil {
		return err
	}
	r.removeEmptyParents(from)
	return nil
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

func (r *DirStore) Tree(ctx context.Context, dir string, depth int, page PageRequest) (Page, error) {
	if !fs.ValidPath(dir) {
		return Page{}, fmt.Errorf("invalid directory path %q", dir)
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
	if err != nil {
		return Page{}, notFound(dir, err)
	}
	// WalkDir visits "a/b.md" before "a.md", so restore byte order.
	slices.Sort(names)
	return paginate(names, page), nil
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

// versionOf derives a version from content, so that a DirStore needs no
// metadata beyond the files themselves.
func versionOf(data []byte) Version {
	sum := sha256.Sum256(data)
	return Version(hex.EncodeToString(sum[:]))
}

func validPath(path string) error {
	if !fs.ValidPath(path) || path == "." {
		return fmt.Errorf("invalid document path %q", path)
	}
	return nil
}

func notFound(path string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNotFound, path)
	}
	return err
}
