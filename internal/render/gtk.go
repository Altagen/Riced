package render

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/Altagen/Riced/internal/manifest"
)

// gtkData is the view-model handed to the GTK CSS template.
type gtkData struct {
	Name    string
	Palette ResolvedPalette
}

// GTKCSS produces a GTK 3 / GTK 4 / libadwaita-compatible CSS file that
// remaps the named colors used by every GTK app to match the manifest
// palette. The same bytes are appropriate for both ~/.config/gtk-3.0/gtk.css
// and ~/.config/gtk-4.0/gtk.css -- libadwaita honours @define-color from
// gtk-4.0/gtk.css for its own named-color overrides.
//
// Never writes to disk.
func GTKCSS(m *manifest.Manifest) ([]byte, error) {
	tmpl, err := template.
		New("gtk.css.tmpl").
		Funcs(FuncMap()).
		ParseFS(templatesFS, "templates/gtk.css.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parse gtk template: %w", err)
	}

	data := gtkData{
		Name:    m.Meta.Name,
		Palette: Resolve(m.Palette),
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute gtk template: %w", err)
	}
	return buf.Bytes(), nil
}
