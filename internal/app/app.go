// Package app holds the use cases shared by the CLI and the MCP server, so both
// surfaces behave identically. Results are plain structs (with JSON tags for
// MCP); presentation is left to the caller.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/darkcarard/sdd-framework/internal/config"
	"github.com/darkcarard/sdd-framework/internal/diag"
	"github.com/darkcarard/sdd-framework/internal/feature"
	"github.com/darkcarard/sdd-framework/internal/implement"
	"github.com/darkcarard/sdd-framework/internal/journal"
	"github.com/darkcarard/sdd-framework/internal/lock"
	"github.com/darkcarard/sdd-framework/internal/plan"
	"github.com/darkcarard/sdd-framework/internal/spec"
	"github.com/darkcarard/sdd-framework/internal/state"
	"github.com/darkcarard/sdd-framework/internal/trace"
	"github.com/darkcarard/sdd-framework/internal/validate"
	"github.com/darkcarard/sdd-framework/internal/workflow"
)

// App is an initialized project.
type App struct {
	Root   string
	Layout state.Layout
	Config config.Config

	// Who identifies the actor of state changes; nil reads git config.
	Who func(ctx context.Context) (feature.Identity, error)
	// Now returns the current time; nil uses time.Now.
	Now func() time.Time
	// Warn receives non-fatal notices (e.g. an abandoned lock taken over); nil drops them.
	Warn func(msg string)
}

// Open finds the project that contains dir (the nearest sdd.yaml at or above
// it) and loads its configuration.
func Open(dir string) (*App, error) {
	root, err := config.FindRoot(dir)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(root)
	if err != nil {
		return nil, err
	}
	return &App{Root: root, Layout: state.Layout{Root: root}, Config: cfg}, nil
}

func (a *App) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *App) identity(ctx context.Context) (feature.Identity, error) {
	if a.Who != nil {
		return a.Who(ctx)
	}
	return GitIdentity(ctx, a.Root)
}

func (a *App) warn(format string, args ...any) {
	if a.Warn != nil {
		a.Warn(fmt.Sprintf(format, args...))
	}
}

// GitIdentity reads git config user.name and user.email.
func GitIdentity(ctx context.Context, dir string) (feature.Identity, error) {
	get := func(key string) string {
		cmd := exec.CommandContext(ctx, "git", "config", "--get", key)
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	id := feature.Identity{Name: get("user.name"), Email: get("user.email")}
	if id.Name == "" && id.Email == "" {
		return id, errors.New("git config user.name and user.email are not set: approvals record who approved (run `git config user.email you@example.com`)")
	}
	return id, nil
}

// withLock runs fn while holding the feature lock.
func (a *App) withLock(name string, fn func() error) error {
	lk, err := lock.Acquire(a.Layout.LockFile(name))
	if err != nil {
		return err
	}
	if lk.TookOver != nil {
		a.warn("took over an abandoned lock (%s)", lk.TookOver)
	}
	err = fn()
	return errors.Join(err, lk.Release())
}

// Resolve maps an argument (name, original text or title) to a feature name.
func (a *App) Resolve(arg string) (string, error) { return feature.Resolve(a.Layout, arg) }

// Load loads a feature by name, original text or title.
func (a *App) Load(arg string) (*feature.Feature, error) {
	name, err := a.Resolve(arg)
	if err != nil {
		return nil, err
	}
	return feature.Load(a.Layout, a.Config, name)
}

// features resolves an optional argument to the features to act on.
func (a *App) features(arg string) ([]string, error) {
	if arg != "" {
		name, err := a.Resolve(arg)
		if err != nil {
			return nil, err
		}
		return []string{name}, nil
	}
	return feature.List(a.Layout)
}

// Diagnostic is the JSON form of a diag.Diagnostic.
type Diagnostic struct {
	Severity string `json:"severity"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Message  string `json:"message"`
}

// Report is the outcome of lint or sync checks.
type Report struct {
	Features    int          `json:"features"`
	Files       int          `json:"files,omitempty"`
	Errors      int          `json:"errors"`
	Warnings    int          `json:"warnings"`
	Diagnostics []Diagnostic `json:"diagnostics"`

	list diag.List
}

// List returns the diagnostics in their native form.
func (r Report) List() diag.List { return r.list }

func newReport(features, files int, ds diag.List) Report {
	ds = ds.Sorted()
	r := Report{Features: features, Files: files, Diagnostics: []Diagnostic{}, list: ds}
	r.Errors, r.Warnings = ds.Count()
	for _, d := range ds {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{Severity: string(d.Severity), File: d.File, Line: d.Line, Message: d.Message})
	}
	return r
}

// checkEach runs check on every feature selected by arg; a directory whose
// name is not a valid feature name is reported as an error.
func (a *App) checkEach(arg string, check func(f *feature.Feature) (int, diag.List)) (Report, error) {
	names, err := a.features(arg)
	if err != nil {
		return Report{}, err
	}
	var all diag.List
	files := 0
	for _, n := range names {
		if err := feature.ValidName(n); err != nil {
			all = append(all, diag.Diagnostic{Severity: diag.Error, File: "specs/" + n, Message: err.Error()})
			continue
		}
		f, err := feature.Load(a.Layout, a.Config, n)
		if err != nil {
			return Report{}, err
		}
		n, ds := check(f)
		files += n
		all = append(all, ds...)
	}
	return newReport(len(names), files, all), nil
}

// Lint runs the deterministic checks on the artifacts of one feature (arg) or all.
func (a *App) Lint(arg string) (Report, error) {
	return a.checkEach(arg, func(f *feature.Feature) (int, diag.List) {
		var ds diag.List
		n := 0
		for _, art := range workflow.Artifacts() {
			if f.Files[art].Exists {
				n++
				ds = append(ds, f.Lint(art)...)
			}
		}
		return n, ds
	})
}

// Sync runs the spec↔spec, spec↔plan and (once implementation has started)
// plan↔code checks on one feature (arg) or all.
func (a *App) Sync(ctx context.Context, arg string) (Report, error) {
	var codeErr error
	r, err := a.checkEach(arg, func(f *feature.Feature) (int, diag.List) {
		ds, _, err := validate.CheckCode(ctx, f)
		if err != nil && codeErr == nil {
			codeErr = fmt.Errorf("%s: plan↔code: %w", f.Name, err)
		}
		return 0, append(f.Sync(), ds...)
	})
	return r, errors.Join(err, codeErr)
}

// Summary is one feature in the project overview.
type Summary struct {
	Name       string `json:"name"`
	Title      string `json:"title,omitempty"`
	Phase      string `json:"phase,omitempty"`
	Functional string `json:"functional,omitempty"`
	Technical  string `json:"technical,omitempty"`
	Plan       string `json:"plan,omitempty"`
	Error      string `json:"error,omitempty"`
}

func summarize(f *feature.Feature) Summary {
	return Summary{
		Name: f.Name, Title: f.Title(), Phase: string(f.Phase),
		Functional: string(f.Statuses[workflow.Functional]),
		Technical:  string(f.Statuses[workflow.Technical]),
		Plan:       string(f.Statuses[workflow.Plan]),
	}
}

// Overview summarizes every feature of the project.
func (a *App) Overview() ([]Summary, error) {
	names, err := feature.List(a.Layout)
	if err != nil {
		return nil, err
	}
	out := []Summary{}
	for _, n := range names {
		f, err := feature.Load(a.Layout, a.Config, n)
		if err != nil {
			out = append(out, Summary{Name: n, Error: err.Error()})
			continue
		}
		out = append(out, summarize(f))
	}
	return out, nil
}

// Artifact is the status of one artifact in a Detail.
type Artifact struct {
	File       string             `json:"file"`
	Status     string             `json:"status"`
	Linted     bool               `json:"linted,omitempty"` // lint counts are present
	Errors     int                `json:"errors,omitempty"`
	Warnings   int                `json:"warnings,omitempty"`
	ApprovedAt *time.Time         `json:"approved_at,omitempty"`
	Approval   *workflow.Approval `json:"-"`
}

// Interrupted is a started action with no recorded end.
type Interrupted struct {
	Seq    int       `json:"seq"`
	Action string    `json:"action"`
	Actor  string    `json:"actor"`
	At     time.Time `json:"at"`
}

// Detail is the full status of one feature.
type Detail struct {
	Summary
	Description string `json:"description,omitempty"`
	// StoredPhase is set when state.json lags behind the effective phase.
	StoredPhase string        `json:"stored_phase,omitempty"`
	Artifacts   []Artifact    `json:"artifacts"`
	Interrupted []Interrupted `json:"interrupted,omitempty"`
	Lock        string        `json:"lock,omitempty"`
	Next        string        `json:"next"`
	// Implementation progress (from implementing on).
	Worktree string                `json:"worktree,omitempty"`
	Branch   string                `json:"branch,omitempty"`
	Groups   []implement.GroupView `json:"groups,omitempty"`
}

// Status returns the detailed status of one feature.
func (a *App) Status(arg string) (Detail, error) {
	f, err := a.Load(arg)
	if err != nil {
		return Detail{}, err
	}
	d := Detail{Summary: summarize(f), Description: f.State.Description, Next: f.Next()}
	if len(f.State.Groups) > 0 {
		d.Worktree, d.Branch, d.Groups = f.State.Worktree, f.State.Branch, implement.Progress(f)
	}
	if f.Saved && f.State.Phase != f.Phase {
		d.StoredPhase = string(f.State.Phase)
	}
	for _, art := range workflow.Artifacts() {
		st := f.Statuses[art]
		ai := Artifact{File: feature.FileName(art), Status: string(st)}
		if appr, ok := f.State.Approvals[art]; ok {
			ai.Approval = &appr
			at := appr.At
			ai.ApprovedAt = &at
		}
		if f.Files[art].Exists && st != workflow.StatusApproved {
			ai.Linted = true
			ai.Errors, ai.Warnings = f.Lint(art).Count()
		}
		d.Artifacts = append(d.Artifacts, ai)
	}
	events, err := journal.Read(a.Layout.JournalFile(f.Name))
	if err != nil {
		return d, err
	}
	for _, ev := range journal.Interrupted(events) {
		d.Interrupted = append(d.Interrupted, Interrupted{Seq: ev.Seq, Action: ev.Action, Actor: ev.Actor, At: ev.TS})
	}
	holder, err := lock.Holder(a.Layout.LockFile(f.Name))
	switch {
	case err == nil:
		host, _ := os.Hostname()
		d.Lock = "held by " + holder.String()
		if lock.Abandoned(holder, host) {
			d.Lock += " (abandoned: the process is gone)"
		}
	case !errors.Is(err, fs.ErrNotExist):
		d.Lock = fmt.Sprintf("unreadable (%v)", err)
	}
	return d, nil
}

// Created is the result of creating an artifact.
type Created struct {
	Feature string `json:"feature"`
	Title   string `json:"title"`
	File    string `json:"file"`
	Next    string `json:"next"`
}

// NewSpecInput describes `sdd spec new`.
type NewSpecInput struct {
	// Text is the feature name, its free-text title, or (for --technical) any
	// form that resolves to an existing feature.
	Text        string
	Name        string // explicit feature name (overrides the one derived from Text)
	Title       string
	Description string
	Technical   bool
}

// NewSpec creates a feature's functional spec, or its technical spec.
func (a *App) NewSpec(ctx context.Context, in NewSpecInput) (Created, error) {
	if in.Technical {
		name := in.Name
		var err error
		if name == "" {
			name, err = a.Resolve(in.Text)
		} else {
			err = feature.ValidName(name)
		}
		if err != nil {
			return Created{}, err
		}
		return a.create(ctx, name, workflow.Technical, feature.Info{})
	}
	name, info, err := NewFeatureName(in.Text, in.Name, in.Title, in.Description)
	if err != nil {
		return Created{}, err
	}
	return a.create(ctx, name, workflow.Functional, info)
}

// NewPlan creates a feature's plan.md from the template.
func (a *App) NewPlan(ctx context.Context, arg string) (Created, error) {
	name, err := a.Resolve(arg)
	if err != nil {
		return Created{}, err
	}
	return a.create(ctx, name, workflow.Plan, feature.Info{})
}

// NewFeatureName decides the name and title of a new feature. A valid name is
// used as is; free text is slugified and kept as the title; name overrides the
// derived one.
func NewFeatureName(text, name, title, desc string) (string, feature.Info, error) {
	info := feature.Info{Title: title, Description: desc}
	switch {
	case name != "":
		if err := feature.ValidName(name); err != nil {
			return "", info, err
		}
		if info.Title == "" && text != name {
			info.Title = text
		}
		return name, info, nil
	case feature.ValidName(text) == nil:
		return text, info, nil
	}
	slug, err := feature.Slugify(text)
	if err != nil {
		return "", info, err
	}
	if info.Title == "" {
		info.Title = strings.Join(strings.Fields(text), " ")
	}
	return slug, info, nil
}

func (a *App) create(ctx context.Context, name string, art workflow.Artifact, info feature.Info) (Created, error) {
	who, err := a.identity(ctx)
	if err != nil {
		return Created{}, err
	}
	var out Created
	err = a.withLock(name, func() error {
		f, err := feature.Open(a.Layout, a.Config, name)
		if err != nil {
			return err
		}
		if _, err := f.Create(art, info, who, a.now()); err != nil {
			return err
		}
		out = Created{Feature: f.Name, Title: f.Title(), File: f.Files[art].Rel, Next: f.Next()}
		return nil
	})
	return out, err
}

// Approved is the result of an approval.
type Approved struct {
	Feature         string
	File            string
	Hash            string
	By              string
	Phase           string
	Next            string
	AlreadyApproved bool
	Diagnostics     Report
}

// ErrBlocked is returned (with the diagnostics) when lint or sync errors block an approval.
var ErrBlocked = feature.ErrBlocked

// Approve freezes an artifact. It is a human-only operation: it is not exposed to agents.
func (a *App) Approve(ctx context.Context, arg string, art workflow.Artifact) (Approved, error) {
	name, err := a.Resolve(arg)
	if err != nil {
		return Approved{}, err
	}
	who, err := a.identity(ctx)
	if err != nil {
		return Approved{}, err
	}
	var out Approved
	err = a.withLock(name, func() error {
		f, err := feature.Load(a.Layout, a.Config, name)
		if err != nil {
			return err
		}
		ds, err := f.Approve(art, who, a.now())
		out = Approved{Feature: f.Name, File: f.Files[art].Rel, Diagnostics: newReport(1, 1, ds)}
		switch {
		case errors.Is(err, feature.ErrAlreadyApproved):
			out.AlreadyApproved, out.Hash = true, f.State.Approvals[art].Hash
			return nil
		case err != nil:
			return err
		}
		appr := f.State.Approvals[art]
		out.Hash, out.By, out.Phase, out.Next = appr.Hash, appr.By, string(f.Phase), f.Next()
		return nil
	})
	return out, err
}

// Description is a feature's human-readable mapping.
type Description struct {
	Feature     string    `json:"feature"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
	CreatedBy   string    `json:"created_by,omitempty"`
	Changed     bool      `json:"changed,omitempty"`
}

func describe(f *feature.Feature) Description {
	return Description{Feature: f.Name, Title: f.Title(), Description: f.State.Description,
		CreatedAt: f.State.CreatedAt, CreatedBy: f.State.CreatedBy}
}

// Describe shows (both nil) or changes a feature's title and description.
func (a *App) Describe(ctx context.Context, arg string, title, desc *string) (Description, error) {
	name, err := a.Resolve(arg)
	if err != nil {
		return Description{}, err
	}
	if title == nil && desc == nil {
		f, err := feature.Load(a.Layout, a.Config, name)
		if err != nil {
			return Description{}, err
		}
		return describe(f), nil
	}
	who, err := a.identity(ctx)
	if err != nil {
		return Description{}, err
	}
	var out Description
	err = a.withLock(name, func() error {
		f, err := feature.Load(a.Layout, a.Config, name)
		if err != nil {
			return err
		}
		changed, err := f.Describe(title, desc, who)
		if err != nil {
			return err
		}
		out = describe(f)
		out.Changed = changed
		return nil
	})
	return out, err
}

func (a *App) index(ctx context.Context, name string) (*trace.Index, error) {
	f, err := feature.Load(a.Layout, a.Config, name)
	if err != nil {
		return nil, err
	}
	ix, _, err := validate.Index(ctx, f)
	return ix, err
}

// TraceableID reports whether id can be traced (FR, NFR, AC, ADR, G#, T#.#).
func TraceableID(id string) bool {
	return spec.ValidID(id) || plan.GroupIDRe.MatchString(id) || plan.TaskIDRe.MatchString(id)
}

// Trace finds an ID ("ID" in every feature, or "<feature>/ID").
func (a *App) Trace(ctx context.Context, arg string) ([]trace.Entry, error) {
	id := arg
	var names []string
	var err error
	if feat, rest, ok := strings.Cut(arg, "/"); ok {
		name, err := a.Resolve(feat)
		if err != nil {
			return nil, err
		}
		names, id = []string{name}, rest
	} else if names, err = feature.List(a.Layout); err != nil {
		return nil, err
	}
	if !TraceableID(id) {
		return nil, fmt.Errorf("%q is not a traceable ID (FR-###, NFR-###, AC-###, ADR-###, G#, T#.#)", id)
	}
	var out []trace.Entry
	for _, n := range names {
		ix, err := a.index(ctx, n)
		if err != nil {
			return nil, err
		}
		if e, ok := ix.Lookup(id); ok {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s is not defined in any feature spec or plan", arg)
	}
	return out, nil
}

// Matrix renders the traceability matrix of a feature in markdown.
func (a *App) Matrix(ctx context.Context, arg string) (string, error) {
	name, err := a.Resolve(arg)
	if err != nil {
		return "", err
	}
	ix, err := a.index(ctx, name)
	if err != nil {
		return "", err
	}
	return ix.Matrix(), nil
}
