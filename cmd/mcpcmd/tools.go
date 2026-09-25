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

type treeInput struct {
	Dir   string `json:"dir,omitempty" jsonschema:"directory relative to the bundle root; defaults to the root"`
	Depth *int   `json:"depth,omitempty" jsonschema:"maximum depth, where 1 is the documents directly under dir; omit for no limit"`
}

type searchInput struct {
	Filter map[string]any `json:"filter,omitempty" jsonschema:"frontmatter fields to match, e.g. {\"type\": \"metric\"}; a list field matches if it contains the value; empty matches every document with frontmatter"`
}

type pathsOutput struct {
	Paths []string `json:"paths"`
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
		Description: "List the paths of markdown documents under a directory recursively, like tree -L depth.",
		InputSchema: treeInputSchema(),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in treeInput) (*mcp.CallToolResult, pathsOutput, error) {
		depth := -1
		if in.Depth != nil {
			depth = *in.Depth
		}
		paths, err := b.Tree(ctx, orRoot(in.Dir), depth)
		return nil, pathsOutput{Paths: nonNil(paths)}, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search",
		Description: "Find documents whose frontmatter matches every field in filter.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, pathsOutput, error) {
		paths, err := b.Search(ctx, in.Filter)
		return nil, pathsOutput{Paths: nonNil(paths)}, err
	})
}

// treeInputSchema is the inferred schema for treeInput with depth limited to
// positive values, so that 0 can't be mistaken for "no limit".
func treeInputSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[treeInput](nil)
	if err != nil {
		panic(err)
	}
	schema.Properties["depth"].Minimum = new(1.0)
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
