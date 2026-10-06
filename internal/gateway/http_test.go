package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/yz4230/okf-storage/internal/localbundle"
)

func newGateway(t *testing.T, files map[string]string) *echo.Echo {
	t.Helper()
	dir := t.TempDir()
	for p, content := range files {
		name := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b, err := localbundle.NewLocalBundle(dir)
	if err != nil {
		t.Fatalf("NewLocalBundle() error = %v", err)
	}
	t.Cleanup(func() { b.Close() })
	e := echo.New()
	RegisterHandlers(e, b)
	return e
}

func get(t *testing.T, e *echo.Echo, target string) *http.Response {
	t.Helper()
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec.Result()
}

func body(t *testing.T, res *http.Response) string {
	t.Helper()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertContains(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("body does not contain %q:\n%s", want, got)
		}
	}
}

func assertNotContains(t *testing.T, got string, unwanted ...string) {
	t.Helper()
	for _, s := range unwanted {
		if strings.Contains(got, s) {
			t.Errorf("body contains %q:\n%s", s, got)
		}
	}
}

const revenue = `---
type: Metric
title: Revenue
description: Recognized revenue.
tags: [finance, revenue]
status: draft
resource: https://example.com/revenue
sources:
  - id: policy
    resource: https://wiki.example.com/policy
---
# Definition

Sums [orders](/tables/orders.md) and [neighbors](orders.md).[^policy]

<script>alert(1)</script>

[bad](javascript:alert(1))

[^policy]: Revenue recognition policy
`

var bundleFiles = map[string]string{
	"index.md":         "---\nokf_version: \"0.2\"\n---\n# Directories\n\n* [Tables](tables/) - Tables.\n",
	"metrics/rev.md":   revenue,
	"tables/orders.md": "---\ntype: Table\ntitle: Orders\ndescription: One row per order.\ntags: [sales]\n---\nOne row per completed order.\n",
	"tables/broken.md": "---\nbad: [\n---\nbody\n",
	"tables/logo.svg":  `<svg xmlns="http://www.w3.org/2000/svg"></svg>`,
	"日本語/メモ.md":        "---\ntype: Note\ntitle: メモ\n---\n日本語の\n文章です。\n",
	".git/config":      "secret",
}

func TestDirectory(t *testing.T) {
	e := newGateway(t, bundleFiles)

	t.Run("root renders index.md and lists entries", func(t *testing.T) {
		res := get(t, e, "/")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", res.StatusCode)
		}
		got := body(t, res)
		assertContains(t, got,
			`<h1 id="directories">Directories</h1>`,
			`<a href="tables/">Tables</a>`,
			`href="/metrics/"`,
			`href="/tables/"`,
			`href="/index.md"`,
			`href="/%E6%97%A5%E6%9C%AC%E8%AA%9E/"`,
		)
		assertNotContains(t, got, ".git", "okf_version")
	})

	t.Run("subdirectory lists document titles", func(t *testing.T) {
		got := body(t, get(t, e, "/tables/"))
		assertContains(t, got,
			`href="/tables/orders.md"`,
			`<span class="fw-semibold">Orders</span>`,
			"One row per order.",
			`<span class="badge text-bg-secondary">Table</span>`,
			`<span class="badge text-bg-danger">invalid</span>`,
			`href="/tables/logo.svg"`,
		)
	})

	t.Run("path without trailing slash redirects", func(t *testing.T) {
		res := get(t, e, "/tables")
		if res.StatusCode != http.StatusFound {
			t.Fatalf("status = %d, want 302", res.StatusCode)
		}
		if got := res.Header.Get("Location"); got != "/tables/" {
			t.Errorf("Location = %q, want /tables/", got)
		}
	})
}

func TestRedirectToCleanPath(t *testing.T) {
	e := newGateway(t, map[string]string{"evil.example/a.md": "x"})
	tests := map[string]string{
		"//evil.example":       "/evil.example",
		"//evil.example/":      "/evil.example/",
		"/./evil.example/a.md": "/evil.example/a.md",
		"/x/../evil.example/":  "/evil.example/",
		"/evil.example":        "/evil.example/",
	}
	for target, want := range tests {
		t.Run(target, func(t *testing.T) {
			res := get(t, e, target)
			if res.StatusCode != http.StatusFound {
				t.Fatalf("status = %d, want 302", res.StatusCode)
			}
			if got := res.Header.Get("Location"); got != want {
				t.Errorf("Location = %q, want %q", got, want)
			}
		})
	}
}

func TestDocument(t *testing.T) {
	e := newGateway(t, bundleFiles)

	t.Run("renders frontmatter and body", func(t *testing.T) {
		res := get(t, e, "/metrics/rev.md")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", res.StatusCode)
		}
		got := body(t, res)
		assertContains(t, got,
			`<h1 class="h3 mb-0">Revenue</h1>`,
			`href="/search?type=Metric"`,
			`text-bg-warning">draft<`,
			"Recognized revenue.",
			`href="/search?tag=finance"`,
			`<a href="https://example.com/revenue" rel="noreferrer">`,
			"- id: policy",
			`<h1 id="definition">Definition</h1>`,
			`<a href="/tables/orders.md">orders</a>`,
			`<a href="orders.md">neighbors</a>`,
			`class="footnotes"`,
			`href="/metrics/"`,
		)
		assertNotContains(t, got, "<script>alert", "javascript:")
	})

	t.Run("joins CJK lines", func(t *testing.T) {
		assertContains(t, body(t, get(t, e, "/日本語/メモ.md")), "<p>日本語の文章です。</p>")
	})

	t.Run("shows an invalid document as is", func(t *testing.T) {
		res := get(t, e, "/tables/broken.md")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", res.StatusCode)
		}
		assertContains(t, body(t, res), "could not be parsed", "invalid frontmatter", "bad: [")
	})
}

func TestFile(t *testing.T) {
	e := newGateway(t, bundleFiles)
	res := get(t, e, "/tables/logo.svg")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if got := res.Header.Get("Content-Type"); got != "image/svg+xml" {
		t.Errorf("Content-Type = %q, want image/svg+xml", got)
	}
	if got := res.Header.Get("Content-Security-Policy"); got != "sandbox" {
		t.Errorf("Content-Security-Policy = %q, want sandbox", got)
	}
}

func TestNotFound(t *testing.T) {
	e := newGateway(t, bundleFiles)
	for _, target := range []string{"/missing.md", "/missing/", "/missing", "/.git/config", "/.git/", "/tables/orders.md/"} {
		t.Run(target, func(t *testing.T) {
			res := get(t, e, target)
			if res.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", res.StatusCode)
			}
			got := body(t, res)
			assertContains(t, got, "404")
			assertNotContains(t, got, "secret")
		})
	}
}

func TestSearch(t *testing.T) {
	e := newGateway(t, bundleFiles)
	tests := map[string]struct {
		query   string
		want    []string
		unwants []string
	}{
		"text is case-insensitive and literal": {
			query: "q=SUMS+%5Borders",
			want:  []string{`href="/metrics/rev.md"`, "1 document<"},
		},
		"tag": {
			query:   "tag=sales",
			want:    []string{`href="/tables/orders.md"`},
			unwants: []string{`href="/metrics/rev.md"`},
		},
		"type and text intersect": {
			query: "type=Table&q=revenue",
			want:  []string{"0 documents"},
		},
		"no criteria": {
			query:   "",
			unwants: []string{"documents<", "list-group-item"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			res := get(t, e, "/search?"+tt.query)
			if res.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", res.StatusCode)
			}
			got := body(t, res)
			assertContains(t, got, tt.want...)
			assertNotContains(t, got, tt.unwants...)
		})
	}
}
