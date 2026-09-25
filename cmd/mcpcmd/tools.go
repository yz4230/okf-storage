package mcpcmd

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yz4230/okf-storage/internal/bundle"
)

type pathInput struct {
	Path string `json:"path" jsonschema:"slash-separated document path relative to the bundle root, e.g. metrics/revenue.md"`
}

type writeInput struct {
	Path    string `json:"path" jsonschema:"slash-separated document path relative to the bundle root, e.g. metrics/revenue.md"`
	Content string `json:"content" jsonschema:"full markdown content of the document, including any YAML frontmatter"`
}

type editInput struct {
	Path       string `json:"path" jsonschema:"slash-separated document path relative to the bundle root"`
	OldString  string `json:"old_string" jsonschema:"exact text to replace; must occur exactly once unless replace_all is set"`
	NewString  string `json:"new_string" jsonschema:"text to replace old_string with"`
	ReplaceAll bool   `json:"replace_all,omitempty" jsonschema:"replace every occurrence of old_string"`
}

type listInput struct {
	Dir string `json:"dir,omitempty" jsonschema:"directory relative to the bundle root; defaults to the root"`
}

// pageInput selects a page of a path listing.
type pageInput struct {
	After string `json:"after,omitempty" jsonschema:"the next value from the previous call, to continue a listing; omit for the first page"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum number of paths to return; defaults to 100"`
}

// defaultLimit and maxLimit keep one listing small enough for a model's
// context; larger results are read page by page.
const (
	defaultLimit = 100
	maxLimit     = 1000
)

func (in pageInput) request() bundle.PageRequest {
	limit := in.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	return bundle.PageRequest{After: in.After, Limit: min(limit, maxLimit)}
}

type treeInput struct {
	Dir   string `json:"dir,omitempty" jsonschema:"directory relative to the bundle root; defaults to the root"`
	Depth *int   `json:"depth,omitempty" jsonschema:"maximum depth, where 1 is the documents directly under dir; omit for no limit"`
	pageInput
}

type searchInput struct {
	Filter map[string]any `json:"filter,omitempty" jsonschema:"frontmatter fields to match, e.g. {\"type\": \"metric\"}; values must be strings, numbers or booleans and match by type, so 2 does not match \"2\"; a list field matches if it contains the value; empty matches every document with frontmatter"`
	pageInput
}

type pathsOutput struct {
	Paths []string `json:"paths"`
	Next  string   `json:"next,omitempty" jsonschema:"present when more paths follow; pass it as after to get them"`
}

func pathsResult(page bundle.Page, err error) (*mcp.CallToolResult, pathsOutput, error) {
	return nil, pathsOutput{Paths: nonNil(page.Paths), Next: page.Next}, err
}

type listOutput struct {
	Entries []bundle.Entry `json:"entries"`
}

func addTools(s *mcp.Server, b bundle.Bundle) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "read",
		Description: "Read a document from the knowledge bundle.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pathInput) (*mcp.CallToolResult, any, error) {
		content, err := b.Read(ctx, in.Path)
		if err != nil {
			return nil, nil, err
		}
		return textResult(content), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "write",
		Description: "Create or overwrite a document in the knowledge bundle.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in writeInput) (*mcp.CallToolResult, any, error) {
		if err := b.Write(ctx, in.Path, in.Content); err != nil {
			return nil, nil, err
		}
		return textResult(fmt.Sprintf("wrote %s", in.Path)), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "edit",
		Description: "Replace an exact string in a document. old_string must occur exactly once unless replace_all is set.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in editInput) (*mcp.CallToolResult, any, error) {
		if err := b.Edit(ctx, in.Path, in.OldString, in.NewString, in.ReplaceAll); err != nil {
			return nil, nil, err
		}
		return textResult(fmt.Sprintf("edited %s", in.Path)), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete",
		Description: "Delete a document from the knowledge bundle.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pathInput) (*mcp.CallToolResult, any, error) {
		if err := b.Delete(ctx, in.Path); err != nil {
			return nil, nil, err
		}
		return textResult(fmt.Sprintf("deleted %s", in.Path)), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list",
		Description: "List the markdown documents and subdirectories directly under a directory, like ls.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listInput) (*mcp.CallToolResult, listOutput, error) {
		entries, err := b.List(ctx, orRoot(in.Dir))
		return nil, listOutput{Entries: nonNil(entries)}, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "tree",
		Description: "List the paths of markdown documents under a directory recursively, like tree -L depth. Results are paged; if next is returned, call again with it as after.",
		InputSchema: treeInputSchema(),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in treeInput) (*mcp.CallToolResult, pathsOutput, error) {
		depth := -1
		if in.Depth != nil {
			depth = *in.Depth
		}
		return pathsResult(b.Tree(ctx, orRoot(in.Dir), depth, in.request()))
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search",
		Description: "Find documents whose frontmatter matches every field in filter. Results are paged; if next is returned, call again with it as after.",
		InputSchema: inputSchema[searchInput](),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, pathsOutput, error) {
		return pathsResult(b.Search(ctx, in.Filter, in.request()))
	})
}

// treeInputSchema is the inferred schema for treeInput with depth limited to
// positive values, so that 0 can't be mistaken for "no limit".
func treeInputSchema() *jsonschema.Schema {
	schema := inputSchema[treeInput]()
	schema.Properties["depth"].Minimum = new(1.0)
	return schema
}

// inputSchema is the inferred schema for T with the page limit bounded.
func inputSchema[T any]() *jsonschema.Schema {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(err)
	}
	limit := schema.Properties["limit"]
	limit.Minimum = new(1.0)
	limit.Maximum = new(float64(maxLimit))
	return schema
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func orRoot(dir string) string {
	if dir == "" {
		return "."
	}
	return dir
}

// nonNil keeps empty results serialized as [] rather than null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
