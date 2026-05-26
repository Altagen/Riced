package render

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"

	"github.com/Altagen/Riced/internal/manifest"
)

//go:embed templates/*.tmpl
var templatesFS embed.FS

// colorschemeData is what the template receives. It exposes a resolved
// palette plus identity fields so the template never reaches back into the
// raw manifest.
type colorschemeData struct {
	Slug    string
	Name    string
	Palette ResolvedPalette
}

// Colorscheme produces the bytes of a KDE Plasma .colors file from the given
// manifest. It does NOT write anything to disk.
//
// The output format matches what KDE places in
// ~/.local/share/color-schemes/<Name>.colors -- once we choose to install it.
func Colorscheme(m *manifest.Manifest) ([]byte, error) {
	tmpl, err := template.
		New("colorscheme.tmpl").
		Funcs(FuncMap()).
		ParseFS(templatesFS, "templates/colorscheme.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parse colorscheme template: %w", err)
	}

	data := colorschemeData{
		Slug:    m.Meta.Slug,
		Name:    m.Meta.Name,
		Palette: Resolve(m.Palette),
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute colorscheme template: %w", err)
	}
	return buf.Bytes(), nil
}
