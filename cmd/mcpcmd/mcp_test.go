package mcpcmd

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yz4230/okf-storage/internal/bundle"
)

type bearerTransport struct{ token string }

func (t bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(r)
}

func TestHandlerAuth(t *testing.T) {
	store, err := bundle.OpenDir(t.TempDir())
	if err != nil {
		t.Fatalf("OpenDir() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })
	ts := httptest.NewServer(newHandler(bundle.NewBundle(store, bundle.NewMemCatalog()), "/okf", "secret"))
	t.Cleanup(ts.Close)

	t.Run("valid token in path connects", func(t *testing.T) {
		client := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
		cs, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: ts.URL + "/okf/secret"}, nil)
		if err != nil {
			t.Fatalf("Connect() error = %v", err)
		}
		cs.Close()
	})

	t.Run("valid token connects", func(t *testing.T) {
		client := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
		cs, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
			Endpoint:   ts.URL + "/okf",
			HTTPClient: &http.Client{Transport: bearerTransport{"secret"}},
		}, nil)
		if err != nil {
			t.Fatalf("Connect() error = %v", err)
		}
		cs.Close()
	})

	const initialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`
	tests := []struct {
		name, path, authz string
		want              int
	}{
		{"missing token", "/okf", "", http.StatusUnauthorized},
		{"wrong token", "/okf", "Bearer wrong", http.StatusUnauthorized},
		{"other path", "/mcp", "Bearer secret", http.StatusNotFound},
		{"wrong token in path", "/okf/wrong", "", http.StatusUnauthorized},
		{"header wins over path", "/okf/secret", "Bearer wrong", http.StatusUnauthorized},
		{"token only served under path", "/secret", "", http.StatusNotFound},
		{"cross-origin with token", "/okf/secret", "", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, ts.URL+tt.path, strings.NewReader(initialize))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("Origin", "https://chatgpt.com")
			if tt.authz != "" {
				req.Header.Set("Authorization", tt.authz)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != tt.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.want)
			}
		})
	}
}

func TestHandlerWithoutTokenRejectsCrossOrigin(t *testing.T) {
	store, err := bundle.OpenDir(t.TempDir())
	if err != nil {
		t.Fatalf("OpenDir() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })
	ts := httptest.NewServer(newHandler(bundle.NewBundle(store, bundle.NewMemCatalog()), "/mcp", ""))
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

// TestHandlerDiscover replays the server/discover request ChatGPT sends, which
// fails unless protocol 2026-07-28 is offered.
func TestHandlerDiscover(t *testing.T) {
	store, err := bundle.OpenDir(t.TempDir())
	if err != nil {
		t.Fatalf("OpenDir() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })
	ts := httptest.NewServer(newHandler(bundle.NewBundle(store, bundle.NewMemCatalog()), "/mcp", ""))
	t.Cleanup(ts.Close)

	const discover = `{"jsonrpc":"2.0","id":"d","method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"t","version":"0"},"io.modelcontextprotocol/clientCapabilities":{}}}}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(discover))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "server/discover")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"2026-07-28"`) {
		t.Errorf("status = %d, body = %s; want 200 advertising 2026-07-28", resp.StatusCode, body)
	}
}

func TestHandlerDump(t *testing.T) {
	store, err := bundle.OpenDir(t.TempDir())
	if err != nil {
		t.Fatalf("OpenDir() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })
	b := bundle.NewBundle(store, bundle.NewMemCatalog())
	want := map[string]string{
		"index.md":           "# Index\n",
		"metrics/revenue.md": "---\ntype: metric\n---\nRevenue.\n",
	}
	for p, content := range want {
		if err := b.Write(t.Context(), p, content); err != nil {
			t.Fatalf("Write(%q) error = %v", p, err)
		}
	}
	ts := httptest.NewServer(newHandler(b, "/mcp", "secret"))
	t.Cleanup(ts.Close)

	tests := []struct {
		name, path, authz string
		want              int
	}{
		{"missing token", "/dump", "", http.StatusUnauthorized},
		{"wrong token in path", "/dump/wrong", "", http.StatusUnauthorized},
		{"bearer token", "/dump", "Bearer secret", http.StatusOK},
		{"token in path", "/dump/secret", "", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, ts.URL+tt.path, nil)
			if tt.authz != "" {
				req.Header.Set("Authorization", tt.authz)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.want)
			}
			if resp.StatusCode != http.StatusOK {
				return
			}
			zr, err := gzip.NewReader(resp.Body)
			if err != nil {
				t.Fatalf("gzip.NewReader() error = %v", err)
			}
			got := make(map[string]string)
			tr := tar.NewReader(zr)
			for {
				hdr, err := tr.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("tar Next() error = %v", err)
				}
				data, _ := io.ReadAll(tr)
				got[hdr.Name] = string(data)
			}
			if !maps.Equal(got, want) {
				t.Errorf("archive = %v, want %v", got, want)
			}
		})
	}
}
