package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitMessage(t *testing.T) {
	got := CommitMessage("feat(signup): G2 Signup service", Trailers{
		Feature: "signup", Group: "G2", Tasks: []string{"T2.1", "T2.2"},
		Covers: []string{"FR-001", "AC-001"}, RunID: "r1",
	})
	want := "feat(signup): G2 Signup service\n\n" +
		"SDD-Feature: signup\nSDD-Group: G2\nSDD-Tasks: T2.1, T2.2\nSDD-Covers: FR-001, AC-001\nSDD-Run: r1\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if s := (Trailers{Group: "G1"}).String(); s != "SDD-Group: G1\n" {
		t.Fatalf("empty trailers should be omitted, got %q", s)
	}
}

// repo creates a git repository with one commit.
func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	g := Git{Dir: dir}
	ctx := context.Background()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"}, {"config", "user.name", "Test"}, {"config", "user.email", "t@example.com"},
		{"config", "commit.gpgsign", "false"},
	} {
		if _, err := g.Run(ctx, args...); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".sdd/worktrees/\n"), 0o644)
	if _, err := g.Run(ctx, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run(ctx, "commit", "-q", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestWorktreeLifecycle(t *testing.T) {
	ctx := context.Background()
	root := repo(t)
	dir := filepath.Join(root, ".sdd", "worktrees", "signup")

	wt, created, err := Ensure(ctx, root, dir, "signup")
	if err != nil || !created {
		t.Fatalf("Ensure: %v created=%v", err, created)
	}
	base, _ := wt.Head(ctx)
	if _, created, err := Ensure(ctx, root, dir, "signup"); err != nil || created {
		t.Fatalf("second Ensure must reuse: %v created=%v", err, created)
	}

	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	changed, err := wt.Changed(ctx)
	if err != nil || strings.Join(changed, ",") != "README.md,a.go" {
		t.Fatalf("Changed = %v, %v", changed, err)
	}
	c1, err := wt.Checkpoint(ctx, "wip: T1.1")
	if err != nil || c1 == base {
		t.Fatalf("Checkpoint: %v", err)
	}
	if same, _ := wt.Checkpoint(ctx, "nothing"); same != c1 {
		t.Error("a checkpoint without changes must not commit")
	}
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package a\n"), 0o644)
	if _, err := wt.Checkpoint(ctx, "wip: T1.2"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "c.go"), []byte("package a\n"), 0o644) // uncommitted fix
	stat, err := wt.DiffStat(ctx, base)
	if err != nil || !strings.Contains(stat, "c.go") || !strings.Contains(stat, "4 files changed") {
		t.Errorf("DiffStat = %q, %v", stat, err)
	}

	msg := CommitMessage("feat(signup): G1 Domain", Trailers{Feature: "signup", Group: "G1", Tasks: []string{"T1.1", "T1.2"}, RunID: "r1"})
	sha, err := wt.Squash(ctx, base, msg)
	if err != nil {
		t.Fatal(err)
	}
	log, err := wt.Log(ctx, base)
	if err != nil || len(log) != 1 || log[0].SHA != sha || log[0].Subject != "feat(signup): G1 Domain" ||
		log[0].Trailers["SDD-Group"] != "G1" || log[0].Trailers["SDD-Tasks"] != "T1.1, T1.2" {
		t.Fatalf("Log = %+v, %v", log, err)
	}
	if changed, _ := wt.Changed(ctx); len(changed) != 0 {
		t.Errorf("the squash must include uncommitted changes: %v", changed)
	}
	// The user's branch is untouched.
	if out, _ := (Git{Dir: root}).Run(ctx, "log", "--oneline", "main"); strings.Count(out, "\n") != 0 {
		t.Errorf("main changed:\n%s", out)
	}

	os.WriteFile(filepath.Join(dir, "junk.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("broken\n"), 0o644)
	if err := wt.Reset(ctx, sha); err != nil {
		t.Fatal(err)
	}
	if changed, _ := wt.Changed(ctx); len(changed) != 0 {
		t.Errorf("Reset left changes: %v", changed)
	}

	// A deleted worktree is recreated from the branch, with its commits.
	if err := Remove(ctx, root, dir, true); err != nil {
		t.Fatal(err)
	}
	if wt.Exists() {
		t.Fatal("Remove left the directory")
	}
	wt, created, err = Ensure(ctx, root, dir, "signup")
	if err != nil || !created {
		t.Fatalf("re-Ensure: %v", err)
	}
	if head, _ := wt.Head(ctx); head != sha {
		t.Errorf("recreated worktree HEAD = %s, want %s", head, sha)
	}
}

func TestEnsureErrors(t *testing.T) {
	ctx := context.Background()
	empty := t.TempDir()
	if _, err := (Git{Dir: empty}).Run(ctx, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Ensure(ctx, empty, filepath.Join(empty, "wt"), "x"); !errors.Is(err, ErrNoCommits) {
		t.Errorf("no commits: %v", err)
	}
	if _, _, err := Ensure(ctx, t.TempDir(), filepath.Join(t.TempDir(), "wt"), "x"); err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("no repo: %v", err)
	}
}

func TestCommitPaths(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	g := Git{Dir: dir}
	if _, err := g.CommitPaths(ctx, "x", "specs"); !errors.Is(err, ErrNotRepo) {
		t.Fatalf("outside a repository: %v", err)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.name", "Test"},
		{"config", "user.email", "t@example.com"}, {"config", "commit.gpgsign", "false"}} {
		if _, err := g.Run(ctx, args...); err != nil {
			t.Fatal(err)
		}
	}
	put := func(rel, content string) {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755)
		os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644)
	}
	// The first commit of a repository; a file the user staged stays staged and out of it.
	put("specs/a/functional.md", "v1\n")
	put("other.go", "package other\n")
	if _, err := g.Run(ctx, "add", "other.go"); err != nil {
		t.Fatal(err)
	}
	sha, err := g.CommitPaths(ctx, "approve", "specs/a", ".sdd/features/a")
	if err != nil || sha == "" {
		t.Fatalf("CommitPaths: %q %v", sha, err)
	}
	if files, _ := g.Run(ctx, "show", "--name-only", "--format=", "HEAD"); files != "specs/a/functional.md" {
		t.Errorf("committed files = %q", files)
	}
	if staged, _ := g.Run(ctx, "diff", "--cached", "--name-only"); staged != "other.go" {
		t.Errorf("staged after commit = %q", staged)
	}
	// Nothing changed: no commit.
	if sha, err := g.CommitPaths(ctx, "again", "specs/a"); err != nil || sha != "" {
		t.Errorf("no changes: %q %v", sha, err)
	}
	// Deletions are committed too.
	put(".sdd/features/a/state.json", "{}\n")
	os.Remove(filepath.Join(dir, "specs/a/functional.md"))
	if sha, err := g.CommitPaths(ctx, "records", "specs/a", ".sdd/features/a"); err != nil || sha == "" {
		t.Fatalf("second commit: %q %v", sha, err)
	}
	if files, _ := g.Run(ctx, "show", "--name-status", "--format=", "HEAD"); !strings.Contains(files, "A\t.sdd/features/a/state.json") ||
		!strings.Contains(files, "D\tspecs/a/functional.md") || strings.Contains(files, "other.go") {
		t.Errorf("second commit files = %q", files)
	}
}

func TestMergeBranch(t *testing.T) {
	ctx := context.Background()
	root := repo(t)
	g := Git{Dir: root}
	dir := filepath.Join(root, ".sdd", "worktrees", "signup")
	wt, _, err := Ensure(ctx, root, dir, "signup")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644)
	if _, err := wt.Checkpoint(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if b, err := g.CurrentBranch(ctx); err != nil || b != "main" {
		t.Fatalf("CurrentBranch = %q %v", b, err)
	}
	ff, err := g.Merge(ctx, "sdd/signup", "merge")
	if err != nil || !ff {
		t.Fatalf("fast-forward: %v %v", ff, err)
	}

	// Diverged: a merge commit.
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package b\n"), 0o644)
	wt.Checkpoint(ctx, "b")
	os.WriteFile(filepath.Join(root, "c.go"), []byte("package c\n"), 0o644)
	g.Run(ctx, "add", "c.go")
	g.Run(ctx, "commit", "-q", "-m", "c")
	if ff, err := g.Merge(ctx, "sdd/signup", "Merge sdd/signup"); err != nil || ff {
		t.Fatalf("merge commit: %v %v", ff, err)
	}
	if subj, _ := g.Run(ctx, "log", "-1", "--format=%s"); subj != "Merge sdd/signup" {
		t.Errorf("merge subject = %q", subj)
	}

	// A conflict is aborted and leaves the checkout clean.
	os.WriteFile(filepath.Join(dir, "d.go"), []byte("package d // branch\n"), 0o644)
	wt.Checkpoint(ctx, "d")
	os.WriteFile(filepath.Join(root, "d.go"), []byte("package d // main\n"), 0o644)
	g.Run(ctx, "add", "d.go")
	g.Run(ctx, "commit", "-q", "-m", "d")
	if _, err := g.Merge(ctx, "sdd/signup", "m"); err == nil {
		t.Fatal("a conflicting merge must fail")
	}
	if st, _ := g.Run(ctx, "status", "--porcelain", "--untracked-files=no"); st != "" {
		t.Errorf("checkout after aborted merge: %q", st)
	}
}
