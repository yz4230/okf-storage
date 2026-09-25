package mcpcmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yz4230/okf-storage/internal/bundle"
	"github.com/yz4230/okf-storage/internal/okf"
)

const (
	// maxIndexBytes caps how much of the root index.md the overview quotes.
	maxIndexBytes = 8 << 10
	// maxValues caps how many values of each vocabulary key are listed.
	maxValues = 50
)

// vocabularyKeys are the frontmatter keys whose values agents most often
// search by; listing the values in use lets them search with exact matches.
var vocabularyKeys = []string{"type", "tags"}

// withOverview appends an overview of the bundle's current contents to the
// instructions of every initialize and discover result, so that agents know
// what to search for before their first call.
func withOverview(b *bundle.Bundle) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			res, err := next(ctx, method, req)
			if err != nil {
				return res, err
			}
			var instr *string
			switch r := res.(type) {
			case *mcp.InitializeResult:
				instr = &r.Instructions
			case *mcp.DiscoverResult:
				instr = &r.Instructions
			default:
				return res, nil
			}
			o, err := overview(ctx, b)
			if err != nil {
				slog.Warn("leaving bundle overview out of instructions", "error", err)
				return res, nil
			}
			*instr += "\n" + o
			return res, nil
		}
	}
}

// overview describes the bundle: how many documents it has, its root
// index.md (or top-level entries without one) and the vocabulary in use.
func overview(ctx context.Context, b *bundle.Bundle) (string, error) {
	var sb strings.Builder
	sb.WriteString("## Current contents\n\n")

	counts := make(map[string]map[string]int)
	n := 0
	var req bundle.PageRequest
	for {
		page, err := b.Search(ctx, nil, req)
		if err != nil {
			return "", err
		}
		for _, path := range page.Paths {
			content, err := b.Read(ctx, path)
			if errors.Is(err, bundle.ErrNotFound) {
				continue
			}
			if err != nil {
				return "", err
			}
			doc, err := okf.ParseDocument(content)
			if err != nil {
				continue
			}
			n++
			for _, key := range vocabularyKeys {
				for _, v := range values(doc.Frontmatter[key]) {
					if counts[key] == nil {
						counts[key] = make(map[string]int)
					}
					counts[key][v]++
				}
			}
		}
		if page.Next == "" {
			break
		}
		req.After = page.Next
	}
	if n == 0 {
		sb.WriteString("The bundle has no documents with frontmatter yet.\n")
		return sb.String(), nil
	}
	fmt.Fprintf(&sb, "The bundle has %d documents with frontmatter.\n", n)

	index, err := b.Read(ctx, "index.md")
	switch {
	case err == nil:
		body := index
		if doc, err := okf.ParseDocument(index); err == nil {
			body = doc.Body
		}
		body = strings.TrimSpace(body)
		if len(body) > maxIndexBytes {
			body = strings.ToValidUTF8(body[:maxIndexBytes], "") + "\n\n(truncated; read index.md for the rest)"
		}
		fmt.Fprintf(&sb, "\nThe root index.md:\n\n<index.md>\n%s\n</index.md>\n", body)
	case errors.Is(err, bundle.ErrNotFound):
		entries, err := b.List(ctx, ".")
		if err != nil {
			return "", err
		}
		sb.WriteString("\nTop-level entries (there is no root index.md):\n\n")
		for _, e := range entries {
			if e.IsDir {
				e.Path += "/"
			}
			fmt.Fprintf(&sb, "- %s\n", e.Path)
		}
	default:
		return "", err
	}

	if len(counts) > 0 {
		sb.WriteString("\nFrontmatter values in use, with document counts. `search` matches exactly, so use these verbatim:\n\n")
		for _, key := range vocabularyKeys {
			if c := counts[key]; c != nil {
				fmt.Fprintf(&sb, "- `%s`: %s\n", key, formatCounts(c))
			}
		}
	}
	return sb.String(), nil
}

// values returns the string values of a frontmatter field: the field itself
// if it is a string, or its string elements if it is a list.
func values(v any) []string {
	switch v := v.(type) {
	case string:
		return []string{v}
	case []any:
		var out []string
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// formatCounts lists values by descending count, then byte order, keeping at
// most maxValues of them.
func formatCounts(counts map[string]int) string {
	vals := slices.SortedFunc(maps.Keys(counts), func(a, b string) int {
		return cmp.Or(cmp.Compare(counts[b], counts[a]), strings.Compare(a, b))
	})
	var parts []string
	for _, v := range vals[:min(len(vals), maxValues)] {
		parts = append(parts, fmt.Sprintf("%s (%d)", v, counts[v]))
	}
	s := strings.Join(parts, ", ")
	if len(vals) > maxValues {
		s += fmt.Sprintf(", and %d more", len(vals)-maxValues)
	}
	return s
}
