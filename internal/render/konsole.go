package render

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/Altagen/Riced/internal/manifest"
)

// konsoleSchemeData is the view-model handed to the colorscheme template.
type konsoleSchemeData struct {
	Name    string
	Term    TerminalColors
	Opacity float64
}

// konsoleProfileData is the view-model handed to the profile template.
type konsoleProfileData struct {
	Slug     string
	Name     string
	MonoFont string
}

// defaultMonoFont is used when the manifest does not specify [fonts].mono.
// Konsole expects the family to be installed on the target system; we pick a
// widely available monospace family rather than silently substituting.
const defaultMonoFont = "Monospace"

// KonsoleColorscheme returns the bytes of a Konsole .colorscheme file
// (the 16-color palette + opacity + blur block). Never writes to disk.
func KonsoleColorscheme(m *manifest.Manifest) ([]byte, error) {
	tmpl, err := template.
		New("konsole.colorscheme.tmpl").
		Funcs(FuncMap()).
		ParseFS(templatesFS, "templates/konsole.colorscheme.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parse konsole colorscheme template: %w", err)
	}

	opacity := m.Terminal.Opacity
	if opacity == 0 {
		opacity = 1.0
	}

	data := konsoleSchemeData{
		Name:    m.Meta.Name,
		Term:    ResolveTerminal(m.Palette),
		Opacity: opacity,
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute konsole colorscheme template: %w", err)
	}
	return buf.Bytes(), nil
}

// KonsoleProfile returns the bytes of a Konsole .profile file pointing at
// the colorscheme of the same slug. Never writes to disk.
func KonsoleProfile(m *manifest.Manifest) ([]byte, error) {
	tmpl, err := template.
		New("konsole.profile.tmpl").
		Funcs(FuncMap()).
		ParseFS(templatesFS, "templates/konsole.profile.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parse konsole profile template: %w", err)
	}

	mono := m.Fonts.Mono
	if mono == "" {
		mono = defaultMonoFont
	}

	data := konsoleProfileData{
		Slug:     m.Meta.Slug,
		Name:     m.Meta.Name,
		MonoFont: mono,
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute konsole profile template: %w", err)
	}
	return buf.Bytes(), nil
}
