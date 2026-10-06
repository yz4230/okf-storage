package gateway

import (
	"bytes"
	"html/template"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

// markdown renders document bodies. Raw HTML in a document is omitted and
// dangerous link URLs (such as javascript:) are dropped, so a document cannot
// inject script into the page. Links are left as written: pages are served
// at the document's own path, so both relative and bundle-relative (leading
// "/") links resolve to the linked document.
var markdown = goldmark.New(
	goldmark.WithExtensions(
		extension.GFM,
		extension.Footnote,
		extension.DefinitionList,
		extension.CJK,
	),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
)

func renderMarkdown(body string) (template.HTML, error) {
	var buf bytes.Buffer
	if err := markdown.Convert([]byte(body), &buf); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}
