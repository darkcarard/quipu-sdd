package feature

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/darkcarard/sdd-framework/internal/config"
	"github.com/darkcarard/sdd-framework/internal/diag"
	"github.com/darkcarard/sdd-framework/internal/journal"
	"github.com/darkcarard/sdd-framework/internal/markdown"
	"github.com/darkcarard/sdd-framework/internal/spec"
	"github.com/darkcarard/sdd-framework/internal/state"
	"github.com/darkcarard/sdd-framework/internal/templates"
	"github.com/darkcarard/sdd-framework/internal/workflow"
)

// Identity is the person performing an action (from git config user.name/email).
type Identity struct {
	Name  string
	Email string
}

// Actor returns the journal actor, "user:<email>" (or the name when there is no email).
func (i Identity) Actor() string {
	if i.Email != "" {
		return "user:" + i.Email
	}
	return "user:" + i.Name
}

func (i Identity) String() string {
	switch {
	case i.Name != "" && i.Email != "":
		return fmt.Sprintf("%s <%s>", i.Name, i.Email)
	case i.Email != "":
		return i.Email
	}
	return i.Name
}

var (
	// ErrAlreadyApproved is returned when approving an artifact whose approval is current.
	ErrAlreadyApproved = errors.New("already approved")
	// ErrBlocked is returned when lint or sync errors prevent an approval.
	ErrBlocked = errors.New("approval blocked by errors")
)

// Open is like Load, but returns a new, unsaved feature when it does not exist yet.
func Open(l state.Layout, cfg config.Config, name string) (*Feature, error) {
	f, err := Load(l, cfg, name)
	if !errors.Is(err, ErrNotFound) {
		return f, err
	}
	f = &Feature{Name: name, Layout: l, Config: cfg, State: state.NewFeatureState(name)}
	return f, f.reload()
}

// Create writes an artifact from its template (the project copy in .sdd/templates
// when present, else the built-in one). The technical spec requires an approved
// functional spec, and the plan an approved technical spec. Source hashes are
// stamped into the new file. It returns the path written.
func (f *Feature) Create(a workflow.Artifact, title string, who Identity, now time.Time) (string, error) {
	file := f.Files[a]
	if file.Exists {
		return "", fmt.Errorf("%s already exists", file.Rel)
	}
	if err := f.requireSources(a); err != nil {
		return "", err
	}
	if title == "" && f.Functional != nil {
		title = f.Functional.Meta.Title
	}
	if title == "" {
		title = TitleFromName(f.Name)
	}
	tpl, err := f.template(FileName(a))
	if err != nil {
		return "", err
	}
	content := []byte(templates.Render(tpl, map[string]string{"feature": f.Name, "title": title}))
	if content, err = f.stamp(a, content); err != nil {
		return "", fmt.Errorf("%s template: %w", FileName(a), err)
	}
	if err := os.MkdirAll(filepath.Dir(file.Path), 0o755); err != nil {
		return "", err
	}
	if err := state.WriteFileAtomic(file.Path, content, 0o644); err != nil {
		return "", err
	}
	if err := f.reload(); err != nil {
		return "", err
	}
	f.State.Phase = f.Phase
	if err := f.Layout.Save(f.State); err != nil {
		return "", err
	}
	f.Saved = true
	action := "spec.create"
	if a == workflow.Plan {
		action = "plan.create"
	}
	if err := f.record(journal.Event{
		Actor: who.Actor(), Phase: string(f.Phase), Action: action, Status: journal.Info,
		Hashes: map[string]string{string(a): f.Files[a].Hash}, Detail: "created " + file.Rel + " from template",
	}); err != nil {
		return "", err
	}
	return file.Path, nil
}

// TitleFromName turns a kebab-case feature name into a title ("user-signup" → "User Signup").
func TitleFromName(name string) string {
	words := strings.Split(name, "-")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func (f *Feature) template(name string) (string, error) {
	b, err := os.ReadFile(filepath.Join(f.Layout.TemplatesDir(), name))
	if err == nil {
		return string(b), nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return templates.Get(name)
}

// requireSources checks that every source of a is approved and current.
func (f *Feature) requireSources(a workflow.Artifact) error {
	for _, src := range workflow.Sources(a) {
		switch st := f.Statuses[src]; st {
		case workflow.StatusApproved:
		case workflow.StatusMissing:
			return fmt.Errorf("%s does not exist yet: start with `%s`", FileName(src), createCommand(f.Name, src))
		default:
			return fmt.Errorf("%s is %s: approve it first with `%s`", FileName(src), st, ApproveCommand(f.Name, src))
		}
	}
	return nil
}

func createCommand(feature string, a workflow.Artifact) string {
	switch a {
	case workflow.Technical:
		return "sdd spec new " + feature + " --technical"
	case workflow.Plan:
		return "sdd plan new " + feature
	}
	return "sdd spec new " + feature
}

// stamp writes the current hashes of a's sources into its frontmatter.
func (f *Feature) stamp(a workflow.Artifact, content []byte) ([]byte, error) {
	var err error
	for _, src := range workflow.Sources(a) {
		content, err = markdown.SetFrontmatter(content, string(src)+"_hash", f.Files[src].Hash)
		if err != nil {
			return nil, err
		}
	}
	return content, nil
}

// Approve freezes an artifact: it checks that its sources are approved, stamps
// their hashes into its frontmatter, runs lint and the relevant sync checks and,
// when there are no errors, records the approval in state.json and the journal.
// The returned diagnostics include warnings (and errors with ErrBlocked).
func (f *Feature) Approve(a workflow.Artifact, who Identity, now time.Time) (diag.List, error) {
	file := f.Files[a]
	if !file.Exists {
		return nil, fmt.Errorf("%s does not exist: create it with `%s`", file.Rel, createCommand(f.Name, a))
	}
	if err := f.requireSources(a); err != nil {
		return nil, err
	}
	if f.Statuses[a] == workflow.StatusApproved {
		return nil, ErrAlreadyApproved
	}

	jr, err := f.Journal()
	if err != nil {
		return nil, err
	}
	action := "spec.approve"
	if a == workflow.Plan {
		action = "plan.approve"
	}
	started, err := jr.Start(journal.Event{
		Actor: who.Actor(), Phase: string(f.Phase), Action: action, IDs: []string{string(a)},
	})
	if err != nil {
		return nil, err
	}
	fail := func(detail string, cause error) (diag.List, error) {
		_, jerr := jr.End(started, journal.Failed, journal.Event{Detail: detail})
		return nil, errors.Join(cause, jerr, f.RenderLog())
	}

	if len(workflow.Sources(a)) > 0 {
		stamped, err := f.stamp(a, file.Content)
		if err != nil {
			return fail(err.Error(), fmt.Errorf("%s: %w", file.Rel, err))
		}
		if string(stamped) != string(file.Content) {
			if err := state.WriteFileAtomic(file.Path, stamped, 0o644); err != nil {
				return fail(err.Error(), err)
			}
			if err := f.reload(); err != nil {
				return fail(err.Error(), err)
			}
		}
	}

	ds := f.Lint(a)
	if a == workflow.Technical {
		ds = append(ds, spec.CheckTechnical(f.Functional, f.Technical).InFile(file.Rel)...)
	}
	if ds.HasErrors() {
		n, _ := ds.Count()
		_, jerr := jr.End(started, journal.Failed, journal.Event{Detail: fmt.Sprintf("blocked by %d lint/sync errors", n)})
		return ds, errors.Join(ErrBlocked, jerr, f.RenderLog())
	}

	appr := workflow.Approval{Hash: f.Files[a].Hash, By: who.String(), At: now.UTC()}
	hashes := map[string]string{string(a): appr.Hash}
	for _, src := range workflow.Sources(a) {
		if appr.SourceHashes == nil {
			appr.SourceHashes = map[workflow.Artifact]string{}
		}
		appr.SourceHashes[src] = f.Files[src].Hash
		hashes[string(src)] = f.Files[src].Hash
	}
	switch a {
	case workflow.Functional:
		appr.IDs = f.Functional.IDs()
	case workflow.Technical:
		appr.IDs = f.Technical.IDs()
	}
	if f.State.Approvals == nil {
		f.State.Approvals = map[workflow.Artifact]workflow.Approval{}
	}
	f.State.Approvals[a] = appr
	if err := f.reload(); err != nil {
		return fail(err.Error(), err)
	}
	f.State.Phase = f.Phase
	if err := f.Layout.Save(f.State); err != nil {
		return fail(err.Error(), err)
	}
	f.Saved = true
	if _, err := jr.End(started, journal.Completed, journal.Event{
		Phase: string(f.Phase), IDs: append([]string{string(a)}, appr.IDs...), Hashes: hashes,
		Detail: fmt.Sprintf("%s approved by %s", FileName(a), who),
	}); err != nil {
		return ds, err
	}
	return ds, f.RenderLog()
}

// record appends an event to the journal and regenerates log.md.
func (f *Feature) record(ev journal.Event) error {
	jr, err := f.Journal()
	if err != nil {
		return err
	}
	if _, err := jr.Append(ev); err != nil {
		return err
	}
	return f.RenderLog()
}
