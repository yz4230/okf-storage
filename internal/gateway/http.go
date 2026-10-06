// Package gateway serves a read-only web UI for browsing a knowledge bundle.
//
// Pages mirror the bundle layout: a directory is served at its path with a
// trailing slash ("/", "/playbooks/"), a document at its path ("/a/b.md"),
// and any other file as is. Links inside documents therefore resolve without
// rewriting. /search is the one path that does not come from the bundle; a
// directory named "search" is still reachable at "/search/".
package gateway

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"syscall"

	"github.com/goccy/go-yaml"
	"github.com/labstack/echo/v5"
	"github.com/yz4230/okf-storage/internal/localbundle"
	"github.com/yz4230/okf-storage/internal/okf"
)

// Bundle is the part of the knowledge bundle the UI reads. Paths are
// slash-separated and relative to the bundle root.
type Bundle interface {
	List(dir string) ([]string, error)
	Read(path string) (string, error)
	SearchFrontmatter(filter map[string]any) ([]string, error)
	SearchContent(pattern string) ([]string, error)
}

// RegisterHandlers registers the UI for b on e.
func RegisterHandlers(e *echo.Echo, b Bundle) {
	renderer := NewTemplateRenderer()
	e.Renderer = &echo.TemplateRenderer{Template: renderer}
	e.HTTPErrorHandler = errorHandler

	e.GET("/search", func(c *echo.Context) error { return search(c, b) })
	e.GET("/*", func(c *echo.Context) error {
		// URL.Path rather than the wildcard parameter: it is always decoded.
		p := c.Request().URL.Path
		// Redirect to the clean form, so that a redirect never starts with
		// "//", which browsers take for another host.
		clean := path.Clean(p)
		if strings.HasSuffix(p, "/") && clean != "/" {
			clean += "/"
		}
		if clean != p {
			return c.Redirect(http.StatusFound, href(strings.TrimPrefix(clean, "/")))
		}
		switch {
		case strings.HasSuffix(p, "/"):
			return directory(c, b, p)
		case path.Ext(p) == ".md":
			return document(c, b, p)
		default:
			return file(c, b, p)
		}
	})
}

// crumb is one step of the breadcrumb trail from the bundle root to a page.
type crumb struct {
	Name string
	Href string
}

// entry describes a directory, document or other file in a listing.
type entry struct {
	Name        string
	Path        string
	Href        string
	IsDir       bool
	IsDoc       bool
	Title       string
	Description string
	Type        string
	Invalid     bool
}

// field is a frontmatter field shown in the document's metadata table.
// Scalars are shown as Text (linked when they are URLs), anything else as
// YAML.
type field struct {
	Key  string
	Text string
	URL  string
	YAML string
}

func directory(c *echo.Context, b Bundle, p string) error {
	dir := strings.Trim(p, "/")
	names, err := b.List("/" + dir)
	if err != nil {
		return notFound(err, p)
	}

	var dirs, files []entry
	for _, name := range names {
		// Not path.Join, which would drop the "/" that marks a directory.
		p := name
		if dir != "" {
			p = dir + "/" + name
		}
		e := describe(b, p)
		if e.IsDir {
			dirs = append(dirs, e)
		} else {
			files = append(files, e)
		}
	}

	var index any
	if slices.Contains(names, "index.md") {
		if raw, err := b.Read(path.Join("/", dir, "index.md")); err == nil {
			if doc, err := okf.ParseDocument(raw); err == nil {
				index, _ = renderMarkdown(doc.Body)
			}
		}
	}

	title := "Bundle"
	if dir != "" {
		title = path.Base(dir) + "/"
	}
	return c.Render(http.StatusOK, "directory", map[string]any{
		"Title":   title,
		"Path":    "/" + dir,
		"Crumbs":  crumbs(dir),
		"Entries": append(dirs, files...),
		"Index":   index,
	})
}

func document(c *echo.Context, b Bundle, p string) error {
	raw, err := b.Read(p)
	if err != nil {
		return notFound(err, p)
	}
	rel := strings.TrimPrefix(p, "/")
	data := map[string]any{
		"Title":  path.Base(rel),
		"Path":   rel,
		"Crumbs": crumbs(rel),
	}

	doc, err := okf.ParseDocument(raw)
	if err != nil {
		data["Error"] = err.Error()
		data["Raw"] = raw
		return c.Render(http.StatusOK, "document", data)
	}
	body, err := renderMarkdown(doc.Body)
	if err != nil {
		return err
	}
	data["Body"] = body

	if fm := doc.Frontmatter; fm != nil {
		data["HasFrontmatter"] = true
		if title, ok := fm["title"].(string); ok && title != "" {
			data["Title"] = title
		}
		data["Type"] = scalar(fm["type"])
		data["Status"] = scalar(fm["status"])
		data["Description"] = scalar(fm["description"])
		data["Tags"] = tags(fm["tags"])

		var fields []field
		for key, value := range fm {
			switch key {
			case "title", "type", "status", "description", "tags":
				continue
			}
			fields = append(fields, newField(key, value))
		}
		slices.SortFunc(fields, func(a, b field) int { return strings.Compare(a.Key, b.Key) })
		data["Fields"] = fields
	}
	return c.Render(http.StatusOK, "document", data)
}

// file serves a file that is not a document, such as an image a document
// embeds, or redirects a directory path given without its trailing slash.
func file(c *echo.Context, b Bundle, p string) error {
	if _, err := b.List(p); err == nil {
		return c.Redirect(http.StatusFound, href(strings.TrimPrefix(p, "/"))+"/")
	}
	data, err := b.Read(p)
	if err != nil {
		return notFound(err, p)
	}
	contentType := mime.TypeByExtension(path.Ext(p))
	if contentType == "" {
		contentType = http.DetectContentType([]byte(data))
	}
	// The file comes from the bundle, not from this UI: keep an HTML or SVG
	// file from running script with the UI's origin.
	h := c.Response().Header()
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("X-Content-Type-Options", "nosniff")
	return c.Blob(http.StatusOK, contentType, []byte(data))
}

// search finds documents whose body contains q (case-insensitively, as
// plain text) and whose frontmatter has the given type and tag. Empty
// criteria are ignored.
func search(c *echo.Context, b Bundle) error {
	q := strings.TrimSpace(c.QueryParam("q"))
	typ := c.QueryParam("type")
	tag := c.QueryParam("tag")

	var paths []string
	searched := false
	filter := map[string]any{}
	if typ != "" {
		filter["type"] = typ
	}
	if tag != "" {
		filter["tags"] = tag
	}
	if len(filter) > 0 {
		m, err := b.SearchFrontmatter(filter)
		if err != nil {
			return err
		}
		paths, searched = m, true
	}
	if q != "" {
		m, err := b.SearchContent("(?i)" + regexp.QuoteMeta(q))
		if err != nil {
			return err
		}
		if searched {
			m = slices.DeleteFunc(m, func(p string) bool { return !slices.Contains(paths, p) })
		}
		paths, searched = m, true
	}

	results := make([]entry, 0, len(paths))
	for _, p := range paths {
		results = append(results, describe(b, p))
	}
	return c.Render(http.StatusOK, "search", map[string]any{
		"Title":    "Search",
		"Query":    q,
		"Type":     typ,
		"Tag":      tag,
		"Searched": searched,
		"Results":  results,
	})
}

// describe returns the listing entry for the bundle path p; a directory's
// path ends with "/". A document's title, description and type come from its
// frontmatter; a document that fails to parse is marked Invalid.
func describe(b Bundle, p string) entry {
	name, isDir := strings.CutSuffix(p, "/")
	e := entry{Name: path.Base(name), Path: p, Href: href(name), IsDir: isDir}
	if isDir {
		e.Href += "/"
		return e
	}
	if path.Ext(p) != ".md" {
		return e
	}
	e.IsDoc = true
	raw, err := b.Read(p)
	if err != nil {
		e.Invalid = true
		return e
	}
	doc, err := okf.ParseDocument(raw)
	if err != nil {
		e.Invalid = true
		return e
	}
	e.Title = scalar(doc.Frontmatter["title"])
	e.Description = scalar(doc.Frontmatter["description"])
	e.Type = scalar(doc.Frontmatter["type"])
	return e
}

// crumbs returns the trail from the root to the bundle path rel; the last
// step is the page itself and has no link.
func crumbs(rel string) []crumb {
	trail := []crumb{{Name: "Bundle", Href: "/"}}
	if rel == "" {
		trail[0].Href = ""
		return trail
	}
	elems := strings.Split(rel, "/")
	for i, name := range elems {
		c := crumb{Name: name}
		if i < len(elems)-1 {
			c.Href = href(strings.Join(elems[:i+1], "/")) + "/"
		}
		trail = append(trail, c)
	}
	return trail
}

// href returns the URL path of the bundle path rel, escaped.
func href(rel string) string {
	return (&url.URL{Path: "/" + rel}).EscapedPath()
}

func scalar(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool, int, int64, uint64, float64:
		return fmt.Sprint(v)
	}
	return ""
}

func tags(v any) []string {
	switch v := v.(type) {
	case string:
		return []string{v}
	case []any:
		var out []string
		for _, t := range v {
			if s := scalar(t); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func newField(key string, value any) field {
	f := field{Key: key}
	switch v := value.(type) {
	case string:
		f.Text = v
		if u, err := url.Parse(v); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
			f.URL = v
		}
		return f
	case nil, bool, int, int64, uint64, float64:
		f.Text = fmt.Sprint(v)
		return f
	}
	out, err := yaml.Marshal(value)
	if err != nil {
		f.Text = fmt.Sprint(value)
		return f
	}
	f.YAML = strings.TrimSpace(string(out))
	return f
}

// notFound turns errors for paths that do not name a page into 404s.
func notFound(err error, p string) error {
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, localbundle.ErrHiddenPath) || errors.Is(err, syscall.ENOTDIR) {
		return echo.NewHTTPError(http.StatusNotFound, "Nothing found at "+p).Wrap(err)
	}
	return err
}

// errorHandler renders errors as an HTML page, showing the message of an
// echo.HTTPError and the error itself otherwise: the UI is for local use.
func errorHandler(c *echo.Context, err error) {
	if resp, _ := echo.UnwrapResponse(c.Response()); resp != nil && resp.Committed {
		return
	}
	code := http.StatusInternalServerError
	message := err.Error()
	if sc := echo.StatusCode(err); sc != 0 {
		code = sc
		message = http.StatusText(code)
	}
	if he, ok := errors.AsType[*echo.HTTPError](err); ok && he.Message != "" {
		message = he.Message
	}
	if code >= http.StatusInternalServerError {
		slog.Error("request failed", "path", c.Request().URL.Path, "err", err)
	}
	if err := c.Render(code, "error", map[string]any{
		"Title":   http.StatusText(code),
		"Code":    code,
		"Message": message,
	}); err != nil {
		slog.Error("failed to render error page", "err", err)
	}
}
