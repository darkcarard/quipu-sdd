// Package templates embeds the default spec/plan templates and language
// profiles that `sdd init` copies into a project.
package templates

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed functional.md technical.md plan.md profiles/*.md
var files embed.FS

// Template names.
const (
	Functional = "functional.md"
	Technical  = "technical.md"
	Plan       = "plan.md"
)

// Names returns the spec/plan template names.
func Names() []string { return []string{Functional, Technical, Plan} }

// Get returns the raw content of an embedded file (e.g. "functional.md" or "profiles/go.md").
func Get(name string) (string, error) {
	b, err := files.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("template %q: %w", name, err)
	}
	return string(b), nil
}

// Render substitutes {{key}} placeholders in a template with the given values.
func Render(content string, vars map[string]string) string {
	for k, v := range vars {
		content = strings.ReplaceAll(content, "{{"+k+"}}", v)
	}
	return content
}

// Install copies the embedded templates into templatesDir and the profiles into
// profilesDir. Existing files are preserved unless overwrite is true, so that
// project customizations survive a re-run of `sdd init`. It returns the paths written.
func Install(templatesDir, profilesDir string, overwrite bool) ([]string, error) {
	var written []string
	err := fs.WalkDir(files, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		dst := filepath.Join(templatesDir, path)
		if rel, ok := strings.CutPrefix(path, "profiles/"); ok {
			dst = filepath.Join(profilesDir, rel)
		}
		if !overwrite {
			if _, statErr := os.Stat(dst); statErr == nil {
				return nil
			} else if !errors.Is(statErr, fs.ErrNotExist) {
				return statErr
			}
		}
		b, err := files.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return err
		}
		written = append(written, dst)
		return nil
	})
	return written, err
}
