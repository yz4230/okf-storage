package localbundle

import (
	"archive/tar"
	"cmp"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"log/slog"
	"math/rand/v2"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/yz4230/okf-storage/internal/okf"
)

var ErrHiddenPath = errors.New("hidden files and directories are not accessible")

const (
	lockName               = ".okf.lock"
	lockTimeout            = 10 * time.Second
	lockRetryInitialDelay  = time.Millisecond
	lockRetryMaxMultiplier = 1000
)

type LocalBundle struct {
	root     *os.Root
	lockPath string
}

func NewLocalBundle(dir string) (*LocalBundle, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &LocalBundle{root: root, lockPath: filepath.Join(root.Name(), lockName)}, nil
}

func (b *LocalBundle) rlock() (unlock func(), err error) { return b.lockFile((*flock.Flock).TryRLock) }

func (b *LocalBundle) lock() (unlock func(), err error) { return b.lockFile((*flock.Flock).TryLock) }

// lockFile opens the lock file anew on each call, so the lock also orders
// goroutines within the process.
func (b *LocalBundle) lockFile(try func(*flock.Flock) (bool, error)) (func(), error) {
	fl := flock.New(b.lockPath, flock.SetPermissions(0o644))
	deadline := time.Now().Add(lockTimeout)
	for n, m := 1, 1; ; n++ {
		ok, err := try(fl)
		if err != nil {
			return nil, b.relErr(err)
		}
		if ok {
			break
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, fmt.Errorf("timed out waiting for %s", lockName)
		}
		// Back off for 0.75 to 1.25 times m * lockRetryInitialDelay, as git does.
		wait := lockRetryInitialDelay * time.Duration(m) * time.Duration(750+rand.IntN(500)) / 1000
		time.Sleep(min(wait, remaining))
		m = min(m+2*n+1, lockRetryMaxMultiplier) // (n+1)^2 = n^2 + 2n + 1
	}
	return func() {
		if err := fl.Unlock(); err != nil {
			slog.Error("failed to unlock bundle", "err", b.relErr(err))
		}
	}, nil
}

func (b *LocalBundle) Close() error {
	return b.root.Close()
}

func (b *LocalBundle) List(dir string) ([]string, error) {
	dir, err := cleanPath(dir)
	if err != nil {
		return nil, err
	}

	unlock, err := b.rlock()
	if err != nil {
		return nil, err
	}
	defer unlock()

	entries, err := fs.ReadDir(b.root.FS(), dir)
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
	path, err := cleanPath(path)
	if err != nil {
		return "", err
	}

	unlock, err := b.rlock()
	if err != nil {
		return "", err
	}
	defer unlock()

	data, err := b.root.ReadFile(path)
	if err != nil {
		return "", b.relErr(err)
	}
	return string(data), nil
}

func (b *LocalBundle) Write(path string, content string) (bool, error) {
	path, err := cleanPath(path)
	if err != nil {
		return false, err
	}
	if _, err := okf.ParseDocument(content); err != nil {
		return false, err
	}

	unlock, err := b.lock()
	if err != nil {
		return false, err
	}
	defer unlock()

	var created bool
	if _, err := b.root.Stat(path); errors.Is(err, fs.ErrNotExist) {
		created = true
	} else if err != nil {
		return false, b.relErr(err)
	}
	if err := b.root.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, b.relErr(err)
	}
	if err := writeFile(b.root, path, []byte(content)); err != nil {
		return false, b.relErr(err)
	}
	return created, nil
}

func (b *LocalBundle) Edit(path string, oldString string, newString string, replaceAll bool) error {
	path, err := cleanPath(path)
	if err != nil {
		return err
	}
	if oldString == "" {
		return errors.New("old string must not be empty")
	}
	if oldString == newString {
		return errors.New("old string and new string must differ")
	}

	unlock, err := b.lock()
	if err != nil {
		return err
	}
	defer unlock()

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
	return b.relErr(writeFile(b.root, path, []byte(content)))
}

func (b *LocalBundle) Delete(path string) error {
	path, err := cleanPath(path)
	if err != nil {
		return err
	}

	unlock, err := b.lock()
	if err != nil {
		return err
	}
	defer unlock()

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
	oldPath, oldErr := cleanPath(oldPath)
	newPath, newErr := cleanPath(newPath)
	if err := errors.Join(oldErr, newErr); err != nil {
		return err
	}

	unlock, err := b.lock()
	if err != nil {
		return err
	}
	defer unlock()

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
	unlock, err := b.rlock()
	if err != nil {
		return nil, err
	}
	defer unlock()

	var matches []string
	for p, err := range b.documents() {
		if err != nil {
			return nil, b.relErr(err)
		}
		data, err := b.root.ReadFile(p)
		if err != nil {
			return nil, b.relErr(err)
		}
		doc, err := okf.ParseDocument(string(data))
		if err != nil {
			slog.Warn("skipping invalid document", "path", p, "err", err)
			continue
		}
		if pred(doc) {
			matches = append(matches, p)
		}
	}

	slices.Sort(matches)
	return matches, nil
}

func (b *LocalBundle) Dump(w io.Writer) error {
	unlock, err := b.rlock()
	if err != nil {
		return err
	}
	defer unlock()

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
			if isHidden(d.Name()) {
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

// cleanPath returns p in the canonical form fs.FS and os.Root accept. A leading
// slash makes p relative to the bundle root, so "/" is the root itself and
// "/a.md" is "a.md".
func cleanPath(p string) (string, error) {
	c := path.Clean(p)
	if rel, ok := strings.CutPrefix(c, "/"); ok {
		c = cmp.Or(rel, ".")
	}
	for elem := range strings.SplitSeq(c, "/") {
		if isHidden(elem) {
			return "", fmt.Errorf("%w: %s", ErrHiddenPath, p)
		}
	}
	return c, nil
}

// isHidden reports whether name is a dotfile. The "." and ".." path
// elements are not hidden names.
func isHidden(name string) bool { return name != "." && name != ".." && strings.HasPrefix(name, ".") }

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
