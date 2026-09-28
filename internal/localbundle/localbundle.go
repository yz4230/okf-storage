package localbundle

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yz4230/okf-storage/internal/okf"
	"golang.org/x/sync/errgroup"
)

var ErrHiddenPath = errors.New("hidden files and directories are not accessible")

type LocalBundle struct {
	mu   sync.RWMutex
	root *os.Root
}

func NewLocalBundle(dir string) (*LocalBundle, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &LocalBundle{root: root}, nil
}

func (b *LocalBundle) Close() error {
	return b.root.Close()
}

func (b *LocalBundle) List(path string) ([]string, error) {
	if err := checkPath(path); err != nil {
		return nil, err
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	entries, err := fs.ReadDir(b.root.FS(), path)
	if err != nil {
		return nil, b.relErr(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		switch {
		case isHidden(e.Name()):
		case e.IsDir():
			names = append(names, e.Name()+"/")
		default:
			names = append(names, e.Name())
		}
	}
	return names, nil
}

func (b *LocalBundle) Read(path string) (string, error) {
	if err := checkPath(path); err != nil {
		return "", err
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	data, err := b.root.ReadFile(path)
	if err != nil {
		return "", b.relErr(err)
	}
	return string(data), nil
}

func (b *LocalBundle) Write(path string, content string) (bool, error) {
	if err := checkPath(path); err != nil {
		return false, err
	}
	if _, err := okf.ParseDocument(content); err != nil {
		return false, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	var created bool
	if _, err := b.root.Stat(path); errors.Is(err, fs.ErrNotExist) {
		created = true
	} else if err != nil {
		return false, b.relErr(err)
	}
	if err := b.root.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, b.relErr(err)
	}
	if err := b.root.WriteFile(path, []byte(content), 0o644); err != nil {
		return false, b.relErr(err)
	}
	return created, nil
}

func (b *LocalBundle) Edit(path string, oldString string, newString string, replaceAll bool) error {
	if err := checkPath(path); err != nil {
		return err
	}
	if oldString == "" {
		return errors.New("old string must not be empty")
	}
	if oldString == newString {
		return errors.New("old string and new string must differ")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	data, err := b.root.ReadFile(path)
	if err != nil {
		return b.relErr(err)
	}
	content := string(data)
	switch n := strings.Count(content, oldString); {
	case n == 0:
		return fmt.Errorf("no occurrences of %q in %s", oldString, path)
	case n > 1 && !replaceAll:
		return fmt.Errorf("multiple occurrences of %q in %s", oldString, path)
	}

	content = strings.ReplaceAll(content, oldString, newString)
	if _, err := okf.ParseDocument(content); err != nil {
		return err
	}
	return b.relErr(b.root.WriteFile(path, []byte(content), 0o644))
}

func (b *LocalBundle) Delete(path string) error {
	if err := checkPath(path); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.root.Remove(path); err != nil {
		return b.relErr(err)
	}
	b.removeEmptyParents(path)
	return nil
}

func (b *LocalBundle) removeEmptyParents(p string) {
	for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
		if b.root.Remove(dir) != nil {
			return
		}
	}
}

func (b *LocalBundle) Move(oldPath string, newPath string) error {
	if err := errors.Join(checkPath(oldPath), checkPath(newPath)); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if _, err := b.root.Stat(newPath); err == nil {
		return &fs.PathError{Op: "move", Path: newPath, Err: fs.ErrExist}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return b.relErr(err)
	}
	if err := b.root.MkdirAll(path.Dir(newPath), 0o755); err != nil {
		return b.relErr(err)
	}
	if err := b.root.Rename(oldPath, newPath); err != nil {
		return b.relErr(err)
	}
	b.removeEmptyParents(oldPath)
	return nil
}

func (b *LocalBundle) SearchFrontmatter(filter map[string]any) ([]string, error) {
	return b.search(func(doc *okf.Document) bool {
		if doc.Frontmatter == nil {
			return false
		}
		return match(map[string]any(doc.Frontmatter), filter)
	})
}

func (b *LocalBundle) SearchContent(pattern string) ([]string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return b.search(func(doc *okf.Document) bool { return re.MatchString(doc.Body) })
}

func (b *LocalBundle) search(pred func(*okf.Document) bool) ([]string, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var (
		eg      errgroup.Group
		mu      sync.Mutex
		matches []string
	)
	eg.SetLimit(runtime.GOMAXPROCS(0))
	for p, err := range b.documents() {
		if err != nil {
			return nil, b.relErr(err)
		}
		eg.Go(func() error {
			data, err := b.root.ReadFile(p)
			if err != nil {
				return err
			}
			doc, err := okf.ParseDocument(string(data))
			if err != nil {
				slog.Warn("skipping invalid document", "path", p, "err", err)
				return nil
			}
			if pred(doc) {
				mu.Lock()
				matches = append(matches, p)
				mu.Unlock()
			}
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, b.relErr(err)
	}
	slices.Sort(matches)

	return matches, nil
}

func (b *LocalBundle) Dump(w io.Writer) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	zw := gzip.NewWriter(w)
	tw := tar.NewWriter(zw)
	now := time.Now()
	for p, err := range b.documents() {
		if err != nil {
			return b.relErr(err)
		}
		data, err := b.root.ReadFile(p)
		if err != nil {
			return b.relErr(err)
		}
		hdr := &tar.Header{Name: p, Mode: 0o644, Size: int64(len(data)), ModTime: now}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return zw.Close()
}

func (b *LocalBundle) documents() iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		err := fs.WalkDir(b.root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if p != "." && isHidden(d.Name()) {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() || path.Ext(p) != ".md" {
				return nil
			}
			if !yield(p, nil) {
				return fs.SkipAll
			}
			return nil
		})
		if err != nil {
			yield("", err)
		}
	}
}

func checkPath(p string) error {
	for elem := range strings.SplitSeq(path.Clean(p), "/") {
		if elem != "." && elem != ".." && isHidden(elem) {
			return fmt.Errorf("%w: %s", ErrHiddenPath, p)
		}
	}
	return nil
}

func isHidden(name string) bool {
	return strings.HasPrefix(name, ".")
}

func match(target any, filter any) bool {
	switch t := target.(type) {
	case map[string]any:
		f, ok := filter.(map[string]any)
		if !ok {
			return false
		}
		for k, fv := range f {
			tv, ok := t[k]
			if !ok || !match(tv, fv) {
				return false
			}
		}
		return true
	case []any:
		f, ok := filter.([]any)
		if !ok {
			f = []any{filter}
		}
		for _, fv := range f {
			if !slices.ContainsFunc(t, func(tv any) bool { return match(tv, fv) }) {
				return false
			}
		}
		return true
	default:
		return equal(target, filter)
	}
}

func equal(a, b any) bool {
	if x, ok := number(a); ok {
		y, ok := number(b)
		return ok && x == y
	}
	switch a.(type) {
	case string, bool, nil:
		return a == b
	}
	return false
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case uint64:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

func (b *LocalBundle) relErr(err error) error {
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		pe.Path = b.rel(pe.Path)
	}
	if le, ok := errors.AsType[*os.LinkError](err); ok {
		le.Old, le.New = b.rel(le.Old), b.rel(le.New)
	}
	return err
}

func (b *LocalBundle) rel(p string) string {
	dir := filepath.Clean(b.root.Name()) + string(filepath.Separator)
	if r, ok := strings.CutPrefix(filepath.Clean(p), dir); ok {
		return filepath.ToSlash(r)
	}
	return p
}
