package mcpcmd

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type pathInput struct {
	Path string `json:"path" jsonschema:"slash-separated document path relative to the bundle root, with or without a leading /, e.g. metrics/revenue.md or /metrics/revenue.md"`
}

type writeInput struct {
	Path    string `json:"path" jsonschema:"slash-separated document path relative to the bundle root, with or without a leading /, e.g. metrics/revenue.md or /metrics/revenue.md"`
	Content string `json:"content" jsonschema:"full markdown content of the document, including any YAML frontmatter"`
}

type editInput struct {
	Path       string `json:"path" jsonschema:"slash-separated document path relative to the bundle root, with or without a leading /"`
	OldString  string `json:"old_string" jsonschema:"exact text to replace; must occur exactly once unless replace_all is set"`
	NewString  string `json:"new_string" jsonschema:"text to replace old_string with"`
	ReplaceAll bool   `json:"replace_all,omitempty" jsonschema:"replace every occurrence of old_string"`
}

type moveInput struct {
	From string `json:"from" jsonschema:"path of the document to move"`
	To   string `json:"to" jsonschema:"new path; must not already exist"`
}

type listInput struct {
	Dir string `json:"dir,omitempty" jsonschema:"directory relative to the bundle root, with or without a leading /; defaults to the root"`
}

type searchFrontmatterInput struct {
	Filter map[string]any `json:"filter,omitempty" jsonschema:"frontmatter fields to match, e.g. {\"type\": \"metric\"}; values match by type, so 2 does not match \"2\"; a list field matches if it contains the value (or every value of a list); an object matches nested fields; empty matches every document with frontmatter"`
}

type searchContentInput struct {
	Query string `json:"query" jsonschema:"regular expression (Go RE2 syntax) to find in document bodies, excluding frontmatter"`
}

type pathsOutput struct {
	Paths []string `json:"paths"`
}

func pathsResult(paths []string, err error) (*mcp.CallToolResult, pathsOutput, error) {
	return nil, pathsOutput{Paths: nonNil(paths)}, err
}

type listOutput struct {
	Entries []string `json:"entries" jsonschema:"names of the entries; directories end with /"`
}

func addTools(s *mcp.Server, b bundle) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "read",
		Description: "Read a document from the knowledge bundle. Check relevant documents before answering or starting a task.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(_ context.Context, _ *mcp.CallToolRequest, in pathInput) (*mcp.CallToolResult, any, error) {
		content, err := b.Read(in.Path)
		if err != nil {
			return nil, nil, err
		}
		return textResult(content), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "write",
		Description: "Create a document in the knowledge bundle, or overwrite an existing one with the full content given; it never appends or merges. Before overwriting, read the document and carry over everything you want to keep; for partial changes use edit instead. Use it on your own initiative to record durable knowledge learned in the conversation (decisions, definitions, procedures, facts about systems); read okf://guide before your first change.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(_ context.Context, _ *mcp.CallToolRequest, in writeInput) (*mcp.CallToolResult, any, error) {
		created, err := b.Write(in.Path, in.Content)
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
	}, func(_ context.Context, _ *mcp.CallToolRequest, in editInput) (*mcp.CallToolResult, any, error) {
		if err := b.Edit(in.Path, in.OldString, in.NewString, in.ReplaceAll); err != nil {
			return nil, nil, err
		}
		return textResult(fmt.Sprintf("edited %s", in.Path)), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete",
		Description: "Delete a document from the knowledge bundle. Directories left empty are removed.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), IdempotentHint: true},
	}, func(_ context.Context, _ *mcp.CallToolRequest, in pathInput) (*mcp.CallToolResult, any, error) {
		if err := b.Delete(in.Path); err != nil {
			return nil, nil, err
		}
		return textResult(fmt.Sprintf("deleted %s", in.Path)), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "move",
		Description: "Move or rename a document, keeping its content. Fails if a document already exists at the new path. Links and index entries pointing at the old path are not updated; fix them afterwards.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(false)},
	}, func(_ context.Context, _ *mcp.CallToolRequest, in moveInput) (*mcp.CallToolResult, any, error) {
		if err := b.Move(in.From, in.To); err != nil {
			return nil, nil, err
		}
		return textResult(fmt.Sprintf("moved %s to %s", in.From, in.To)), nil, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list",
		Description: "List the entries directly under a directory, like ls. Directory names end with /.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(_ context.Context, _ *mcp.CallToolRequest, in listInput) (*mcp.CallToolResult, listOutput, error) {
		entries, err := b.List(orRoot(in.Dir))
		return nil, listOutput{Entries: nonNil(entries)}, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search_frontmatter",
		Description: "Find documents whose frontmatter matches every field in filter, e.g. {\"filter\": {\"tags\": \"billing\"}}. Use it to find existing knowledge before answering, and before writing to avoid duplicates.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(_ context.Context, _ *mcp.CallToolRequest, in searchFrontmatterInput) (*mcp.CallToolResult, pathsOutput, error) {
		return pathsResult(b.SearchFrontmatter(in.Filter))
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search_content",
		Description: "Find documents whose body matches the regular expression in query, like grep -l, e.g. {\"query\": \"billing\"}. Use it for knowledge that frontmatter does not describe.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(_ context.Context, _ *mcp.CallToolRequest, in searchContentInput) (*mcp.CallToolResult, pathsOutput, error) {
		return pathsResult(b.SearchContent(in.Query))
	})
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
