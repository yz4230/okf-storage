package gateway

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"path"
)

//go:embed templates
var templateFiles embed.FS

// TemplateRenderer renders each page into the shared base layout.
type TemplateRenderer struct {
	templates map[string]*template.Template
}

func NewTemplateRenderer() *TemplateRenderer {
	const ext = ".html.tmpl"
	base := template.Must(template.New("base").ParseFS(templateFiles, path.Join("templates", "base"+ext)))

	mustParse := func(page string) *template.Template {
		tmpl := template.Must(base.Clone())
		return template.Must(tmpl.ParseFS(templateFiles, path.Join("templates", "pages", page+ext)))
	}

	return &TemplateRenderer{
		templates: map[string]*template.Template{
			"directory": mustParse("directory"),
			"document":  mustParse("document"),
			"search":    mustParse("search"),
			"error":     mustParse("error"),
		},
	}
}

func (r *TemplateRenderer) ExecuteTemplate(wr io.Writer, name string, data any) error {
	tmpl, ok := r.templates[name]
	if !ok {
		return fmt.Errorf("unknown template %q", name)
	}
	return tmpl.ExecuteTemplate(wr, "base", data)
}
