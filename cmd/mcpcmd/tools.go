package mcpcmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

type deleteInput struct {
	Path string `json:"path,omitempty" jsonschema:"path of the document to delete"`
	Dir  string `json:"dir,omitempty" jsonschema:"instead of path, a directory whose documents to delete recursively, e.g. drafts/old"`
}

type moveInput struct {
	From string `json:"from" jsonschema:"path of the document to move"`
	To   string `json:"to" jsonschema:"new path; must not already hold a document"`
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

func addTools(s *mcp.Server, b *bundle.Bundle) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "read",
		Description: "Read a document from the knowledge bundle. Check relevant documents before answering or starting a task.",
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
		Description: "Create a document in the knowledge bundle, or overwrite an existing one with the full content given; it never appends or merges. Before overwriting, read the document and carry over everything you want to keep; for partial changes use edit instead. Use it on your own initiative to record durable knowledge learned in the conversation (decisions, definitions, procedures, facts about systems); read okf://guide before your first change.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in writeInput) (*mcp.CallToolResult, any, error) {
		created, err := b.Write(ctx, in.Path, in.Content)
		if err != nil {
			return nil, nil, err
		}
		if created {
			return textResult(fmt.Sprintf("created %s", in.Path)), nil, nil
		}
		return textResult(fmt.Sprintf("overwrote %s", in.Path)), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "edit",
		Description: "Replace an exact string in a document. old_string must occur exactly once unless replace_all is set. Use it on your own initiative to extend or correct existing knowledge instead of creating duplicates.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in editInput) (*mcp.CallToolResult, any, error) {
		if err := b.Edit(ctx, in.Path, in.OldString, in.NewString, in.ReplaceAll); err != nil {
			return nil, nil, err
		}
		return textResult(fmt.Sprintf("edited %s", in.Path)), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete",
		Description: "Delete a document from the knowledge bundle, or with dir instead of path, every document under a directory recursively.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteInput) (*mcp.CallToolResult, any, error) {
		switch {
		case (in.Path == "") == (in.Dir == ""):
			return nil, nil, errors.New("set exactly one of path and dir")
		case in.Path != "":
			if err := b.Delete(ctx, in.Path); err != nil {
				return nil, nil, err
			}
			return textResult(fmt.Sprintf("deleted %s", in.Path)), nil, nil
		}
		deleted, err := b.DeleteDir(ctx, in.Dir)
		if err != nil {
			return nil, nil, fmt.Errorf("deleted %d documents, then: %w", len(deleted), err)
		}
		return textResult(fmt.Sprintf("deleted %d documents:\n%s", len(deleted), strings.Join(deleted, "\n"))), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "move",
		Description: "Move or rename a document, keeping its content. Fails if a document already exists at the new path. Links and index entries pointing at the old path are not updated; fix them afterwards.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(false)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in moveInput) (*mcp.CallToolResult, any, error) {
		if err := b.Move(ctx, in.From, in.To); err != nil {
			return nil, nil, err
		}
		return textResult(fmt.Sprintf("moved %s to %s", in.From, in.To)), nil, nil
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
		Description: "Find documents whose frontmatter matches every field in filter. Use it to find existing knowledge before answering, and before writing to avoid duplicates. Results are paged; if next is returned, call again with it as after.",
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
