package admin

import (
	"embed"
	"html/template"
	"io"
	"time"
)

//go:embed templates/*.html static/*
var content embed.FS

// Render renders a template with the given data.
func Render(w io.Writer, name string, data any) error {
	tmpl, err := template.New("base.html").Funcs(template.FuncMap{
		"formatTime": func(t time.Time) string {
			return t.Format(time.RFC3339)
		},
	}).ParseFS(content, "templates/base.html", "templates/"+name)
	if err != nil {
		return err
	}
	return tmpl.Execute(w, data)
}
