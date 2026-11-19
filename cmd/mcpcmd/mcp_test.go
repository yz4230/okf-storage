package mcpcmd

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yz4230/mcpauth"
)

type bearerTransport struct{ token string }

func (t bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(r)
}

// newAuthHandler serves b behind OAuth and the static token "static", and
// returns the server with an access token obtained through the full
// authorization code flow.
func newAuthHandler(t *testing.T, b bundle) (*httptest.Server, string) {
	t.Helper()
	var h http.Handler
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) }))
	t.Cleanup(ts.Close)
	a, err := newAuth(t.TempDir(), "secret", mcpauth.Config{PublicURL: ts.URL})
	if err != nil {
		t.Fatalf("newAuth() error = %v", err)
	}
	h = newHandler(b, a, "static")

	const redirect = "https://claude.ai/api/mcp/auth_callback"
	resp, err := http.Post(ts.URL+"/register", "application/json",
		strings.NewReader(`{"redirect_uris":["`+redirect+`"],"client_name":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	var reg struct {
		ClientID string `json:"client_id"`
	}
	json.NewDecoder(resp.Body).Decode(&reg)
	resp.Body.Close()

	const verifier = "test-verifier-test-verifier-test-verifier-0123"
	sum := sha256.Sum256([]byte(verifier))
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err = noRedirect.PostForm(ts.URL+"/authorize", url.Values{
		"client_id": {reg.ClientID}, "redirect_uri": {redirect}, "response_type": {"code"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
		"password": {"secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || loc.Query().Get("code") == "" {
		t.Fatalf("authorize: status %d, Location %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	resp, err = http.PostForm(ts.URL+"/token", url.Values{
		"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")},
		"client_id": {reg.ClientID}, "redirect_uri": {redirect}, "code_verifier": {verifier},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	json.NewDecoder(resp.Body).Decode(&tok)
	if tok.AccessToken == "" {
		t.Fatalf("token: status %d, no access_token", resp.StatusCode)
	}
	return ts, tok.AccessToken
}

func TestHandlerAuth(t *testing.T) {
	ts, token := newAuthHandler(t, newBundle(t))

	t.Run("access token connects", func(t *testing.T) {
		client := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
		cs, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
			Endpoint:   ts.URL + "/mcp",
			HTTPClient: &http.Client{Transport: bearerTransport{token}},
		}, nil)
		if err != nil {
			t.Fatalf("Connect() error = %v", err)
		}
		cs.Close()
	})

	t.Run("missing token points to resource metadata", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/mcp", "application/json", strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(resp.Header.Get("WWW-Authenticate"), "resource_metadata=") {
			t.Errorf("status = %d, WWW-Authenticate = %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
		}
	})

	const initialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`
	tests := []struct {
		name, path, authz string
		want              int
	}{
		{"wrong token", "/mcp", "Bearer wrong", http.StatusUnauthorized},
		{"static token", "/mcp", "Bearer static", http.StatusOK},
		{"token in path is not accepted", "/mcp/" + token, "", http.StatusUnauthorized},
		{"static token in path is not accepted", "/mcp/static", "", http.StatusUnauthorized},
		{"other path", "/other", "Bearer " + token, http.StatusNotFound},
		{"cross-origin with token", "/mcp", "Bearer " + token, http.StatusOK},
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

func TestHandlerStaticTokenOnly(t *testing.T) {
	ts := httptest.NewServer(newHandler(newBundle(t), nil, "static"))
	t.Cleanup(ts.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
	cs, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:   ts.URL + "/mcp",
		HTTPClient: &http.Client{Transport: bearerTransport{"static"}},
	}, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	cs.Close()

	for _, authz := range []string{"", "Bearer wrong"} {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader("{}"))
		if authz != "" {
			req.Header.Set("Authorization", authz)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Authorization %q: status = %d, want 401", authz, resp.StatusCode)
		}
	}
}

func TestNewAuthRequiresBoth(t *testing.T) {
	if _, err := newAuth(t.TempDir(), "secret", mcpauth.Config{}); err == nil {
		t.Error("password without public URL: want error")
	}
	if _, err := newAuth(t.TempDir(), "", mcpauth.Config{PublicURL: "https://example.com"}); err == nil {
		t.Error("public URL without password: want error")
	}
	if a, err := newAuth(t.TempDir(), "", mcpauth.Config{}); a != nil || err != nil {
		t.Errorf("neither: got %v, %v; want nil, nil", a, err)
	}
}

func TestHandlerWithoutAuthRejectsCrossOrigin(t *testing.T) {
	ts := httptest.NewServer(newHandler(newBundle(t), nil, ""))
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
	ts := httptest.NewServer(newHandler(newBundle(t), nil, ""))
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
	b := newBundle(t)
	want := map[string]string{
		"index.md":           "# Index\n",
		"metrics/revenue.md": "---\ntype: metric\n---\nRevenue.\n",
	}
	for p, content := range want {
		if _, err := b.Write(p, content); err != nil {
			t.Fatalf("Write(%q) error = %v", p, err)
		}
	}
	ts, token := newAuthHandler(t, b)

	tests := []struct {
		name, path, authz string
		want              int
	}{
		{"missing token", "/dump", "", http.StatusUnauthorized},
		{"token in path", "/dump/" + token, "", http.StatusUnauthorized},
		{"bearer token", "/dump", "Bearer " + token, http.StatusOK},
		{"static token", "/dump", "Bearer static", http.StatusOK},
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

func TestServeStdio(t *testing.T) {
	dir := t.TempDir()
	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- serveStdio(t.Context(), dir, &mcp.IOTransport{Reader: serverIn, Writer: serverOut})
		serverOut.Close()
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
	cs, err := client.Connect(t.Context(), &mcp.IOTransport{Reader: clientIn, Writer: clientOut}, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "write",
		Arguments: map[string]any{"path": "a.md", "content": "---\ntype: Note\n---\nhi\n"},
	})
	if err != nil || res.IsError {
		t.Fatalf("CallTool(write) = %v, %v", res, err)
	}
	cs.Close()

	if err := <-done; err != nil {
		t.Errorf("serveStdio() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.md")); err != nil {
		t.Errorf("document not written: %v", err)
	}
}
