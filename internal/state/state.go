// Package state manages the on-disk project layout (sdd.yaml, specs/, .sdd/)
// and the per-feature checkpoint file state.json, which is always written atomically.
package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/darkcarard/quipu-sdd/internal/workflow"
)

// Layout resolves every framework path inside a project root.
type Layout struct{ Root string }

func (l Layout) ConfigFile() string         { return filepath.Join(l.Root, "sdd.yaml") }
func (l Layout) GitignoreFile() string      { return filepath.Join(l.SDDDir(), ".gitignore") }
func (l Layout) SpecsDir() string           { return filepath.Join(l.Root, "specs") }
func (l Layout) SpecDir(f string) string    { return filepath.Join(l.SpecsDir(), f) }
func (l Layout) SDDDir() string             { return filepath.Join(l.Root, ".sdd") }
func (l Layout) TemplatesDir() string       { return filepath.Join(l.SDDDir(), "templates") }
func (l Layout) ProfilesDir() string        { return filepath.Join(l.SDDDir(), "profiles") }
func (l Layout) FeaturesDir() string        { return filepath.Join(l.SDDDir(), "features") }
func (l Layout) FeatureDir(f string) string { return filepath.Join(l.FeaturesDir(), f) }
func (l Layout) StateFile(f string) string  { return filepath.Join(l.FeatureDir(f), "state.json") }
func (l Layout) JournalFile(f string) string {
	return filepath.Join(l.FeatureDir(f), "journal.jsonl")
}
func (l Layout) LogFile(f string) string     { return filepath.Join(l.FeatureDir(f), "log.md") }
func (l Layout) SessionsDir(f string) string { return filepath.Join(l.FeatureDir(f), "sessions") }
func (l Layout) ReportFile(f string) string  { return filepath.Join(l.FeatureDir(f), "report.md") }
func (l Layout) RunsDir() string             { return filepath.Join(l.SDDDir(), "runs") }
func (l Layout) RunDir(id string) string     { return filepath.Join(l.RunsDir(), id) }
func (l Layout) WorktreesDir() string        { return filepath.Join(l.SDDDir(), "worktrees") }
func (l Layout) WorktreeDir(f string) string { return filepath.Join(l.WorktreesDir(), f) }
func (l Layout) LocksDir() string            { return filepath.Join(l.SDDDir(), "locks") }
func (l Layout) LockFile(f string) string    { return filepath.Join(l.LocksDir(), f+".lock") }

// Dirs returns every directory created by Init.
func (l Layout) Dirs() []string {
	return []string{
		l.SpecsDir(), l.SDDDir(), l.TemplatesDir(), l.ProfilesDir(),
		l.FeaturesDir(), l.RunsDir(), l.WorktreesDir(), l.LocksDir(),
	}
}

// Gitignore is the content of .sdd/.gitignore: the ephemeral paths under .sdd/
// that must never be committed. Keeping it inside .sdd/ means the project's own
// .gitignore is never modified.
const Gitignore = "# sdd: ephemeral state, never committed\nworktrees/\nlocks/\nruns/\n"

// Init creates the directory layout. It is idempotent.
func (l Layout) Init() error {
	for _, d := range l.Dirs() {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", d, err)
		}
	}
	return nil
}

// WriteFileAtomic writes data to path so that readers observe either the old or
// the new content, never a partial write: temp file in the same directory, fsync,
// rename, then fsync of the directory.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp.Name(), perm); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync() // best effort; not supported on every platform
		d.Close()
	}
	return nil
}

// FeatureState is the checkpoint persisted in .sdd/features/<feature>/state.json.
// See docs/TRACEABILITY.md §3.
type FeatureState struct {
	Feature string `json:"feature"`
	// Title and Description are the human-readable side of the feature name
	// (the sanitized slug in Feature). They are not part of any approved content.
	Title       string    `json:"title,omitempty"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
	CreatedBy   string    `json:"created_by,omitempty"`
	// Origin is "existing" for a baseline feature (adopted code, no plan).
	Origin string `json:"origin,omitempty"`

	Phase     workflow.Phase                          `json:"phase"`
	Approvals map[workflow.Artifact]workflow.Approval `json:"approvals,omitempty"`
	Groups    []GroupState                            `json:"groups,omitempty"`
	Current   Cursor                                  `json:"current"`
	RunID     string                                  `json:"run_id,omitempty"`
	Worktree  string                                  `json:"worktree,omitempty"`
	Branch    string                                  `json:"branch,omitempty"`
	// CheckpointSHA is the last commit known to contain finished work.
	CheckpointSHA string `json:"checkpoint_sha,omitempty"`
	// ValidatedSHA is the branch commit that the last passing validation checked.
	ValidatedSHA string `json:"validated_sha,omitempty"`
	// Merged records `sdd merge`.
	Merged    *Merge    `json:"merged,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Merge records where a validated feature's branch was merged.
type Merge struct {
	Into string    `json:"into"`         // the branch merged into
	SHA  string    `json:"sha"`          // its HEAD after the merge
	FF   bool      `json:"ff,omitempty"` // fast-forward
	By   string    `json:"by"`
	At   time.Time `json:"at"`
}

// Cursor points at the work in progress.
type Cursor struct {
	Group string `json:"group,omitempty"`
	Task  string `json:"task,omitempty"`
}

// GroupState tracks one plan group.
type GroupState struct {
	ID        string               `json:"id"`
	Status    workflow.GroupStatus `json:"status"`
	BaseSHA   string               `json:"base_sha,omitempty"`   // HEAD before the group started (squash base)
	CommitSHA string               `json:"commit_sha,omitempty"` // squashed group commit
	Attempts  int                  `json:"attempts,omitempty"`   // failed verifications since the last success or revision
	Tasks     []TaskState          `json:"tasks,omitempty"`
	// Feedback is the user's latest `sdd revise` message, until verification passes again.
	Feedback  string `json:"feedback,omitempty"`
	Revisions int    `json:"revisions,omitempty"`
	// Verification is the summary of the latest group verification.
	Verification string `json:"verification,omitempty"`
}

// TaskState tracks one plan task.
type TaskState struct {
	ID            string              `json:"id"`
	Status        workflow.TaskStatus `json:"status"`
	Attempts      int                 `json:"attempts,omitempty"`       // failed task_done builds
	CheckpointSHA string              `json:"checkpoint_sha,omitempty"` // WIP commit
	// Summary is what the agent reported when it finished the task.
	Summary string `json:"summary,omitempty"`
	// Undeclared lists changed files the plan did not declare for the task.
	Undeclared []string `json:"undeclared,omitempty"`
}

// NewFeatureState returns the initial state of a new feature.
func NewFeatureState(feature string) FeatureState {
	return FeatureState{Feature: feature, Phase: workflow.FunctionalDrafting}
}

// ErrNoFeature is returned when a feature has no state file.
var ErrNoFeature = errors.New("feature not found")

// Load reads a feature checkpoint.
func (l Layout) Load(feature string) (FeatureState, error) {
	var s FeatureState
	b, err := os.ReadFile(l.StateFile(feature))
	if errors.Is(err, fs.ErrNotExist) {
		return s, fmt.Errorf("%w: %s", ErrNoFeature, feature)
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("parsing %s: %w", l.StateFile(feature), err)
	}
	return s, nil
}

// Save atomically writes a feature checkpoint, stamping UpdatedAt.
func (l Layout) Save(s FeatureState) error {
	if s.Feature == "" {
		return errors.New("state: feature name is required")
	}
	s.UpdatedAt = time.Now().UTC()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // keep "Name <email>" readable in a committed file
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		return err
	}
	return WriteFileAtomic(l.StateFile(s.Feature), buf.Bytes(), 0o644)
}
