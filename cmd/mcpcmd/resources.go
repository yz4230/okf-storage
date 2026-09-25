package mcpcmd

import (
	"context"
	_ "embed"
	"errors"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yz4230/okf-storage/internal/bundle"
)

//go:embed guide.md
var guide string

// spec is OKF v0.2 as published at
// https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/22efaa5402775a7c4d4c37f89e41258daaf3cb65/okf/SPEC.md
// under the Apache License 2.0 (see spec.LICENSE.md), unmodified.
//
//go:embed spec.md
var spec string

const (
	guideURI  = "okf://guide"
	specURI   = "okf://spec"
	docPrefix = "okf://docs/"
)

func addResources(s *mcp.Server, b bundle.Bundle) {
	s.AddResource(&mcp.Resource{
		URI:         guideURI,
		Name:        "guide",
		Title:       "Knowledge organization guide",
		Description: "How to structure, write, link and maintain OKF documents in this bundle. Read before changing the bundle.",
		MIMEType:    "text/markdown",
		Annotations: &mcp.Annotations{Audience: []mcp.Role{"assistant"}, Priority: 1},
	}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return markdownResult(guideURI, guide), nil
	})

	s.AddResource(&mcp.Resource{
		URI:         specURI,
		Name:        "spec",
		Title:       "Open Knowledge Format (OKF) v0.2 specification",
		Description: "The full OKF v0.2 specification that documents in this bundle follow.",
		MIMEType:    "text/markdown",
		Annotations: &mcp.Annotations{Audience: []mcp.Role{"assistant"}, Priority: 0.5},
	}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return markdownResult(specURI, spec), nil
	})

	s.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: docPrefix + "{+path}",
		Name:        "document",
		Title:       "Bundle document",
		Description: "A document in the knowledge bundle by its path relative to the bundle root, e.g. okf://docs/metrics/revenue.md.",
		MIMEType:    "text/markdown",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		uri := req.Params.URI
		path, err := url.PathUnescape(strings.TrimPrefix(uri, docPrefix))
		if err != nil {
			return nil, mcp.ResourceNotFoundError(uri)
		}
		content, err := b.Read(ctx, path)
		if errors.Is(err, bundle.ErrNotFound) {
			return nil, mcp.ResourceNotFoundError(uri)
		}
		if err != nil {
			return nil, err
		}
		return markdownResult(uri, content), nil
	})
}

func markdownResult(uri, text string) *mcp.ReadResourceResult {
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "text/markdown", Text: text}}}
}
