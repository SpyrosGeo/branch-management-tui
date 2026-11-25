package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runGit(dir, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// gitFixture makes an origin repo with develop and feature/remote, and a clone of it.
// The clone is mapped as repo "ws/app".
func gitFixture(t *testing.T) (origin, clone string, repos map[string]Repo) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(k, "t")
	}
	for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "t@t")
	}
	dir := t.TempDir()
	origin, clone = filepath.Join(dir, "origin"), filepath.Join(dir, "clone")
	mustGit(t, dir, "init", "-q", "-b", "develop", origin)
	mustGit(t, origin, "commit", "-q", "--allow-empty", "-m", "init")
	mustGit(t, origin, "branch", "feature/remote")
	mustGit(t, dir, "clone", "-q", origin, clone)
	return origin, clone, map[string]Repo{"ws/app": {Path: clone, Base: "develop"}}
}

func runCtx(a Action, t *Ticket, b *Branch, choice int) (string, error) {
	var note string
	err := a.Run(ActionContext{Ticket: t, Branch: b, Choice: choice, Notify: func(s string) { note = s }})
	return note, err
}

func TestCheckoutFetchesRemoteBranch(t *testing.T) {
	_, clone, repos := gitFixture(t)
	tk := &Ticket{Key: "A-1", Branches: []Branch{{Name: "feature/remote", Repo: "ws/app"}}}
	note, err := runCtx(checkoutBranch(repos), tk, &tk.Branches[0], 0)
	if err != nil {
		t.Fatal(err)
	}
	if head := mustGit(t, clone, "branch", "--show-current"); head != "feature/remote" || !strings.Contains(note, "checked out feature/remote") {
		t.Errorf("head %q, note %q", head, note)
	}
	if up := mustGit(t, clone, "rev-parse", "--abbrev-ref", "feature/remote@{upstream}"); up != "origin/feature/remote" {
		t.Errorf("upstream %q", up)
	}
	// Second time the branch is local: plain checkout.
	mustGit(t, clone, "checkout", "-q", "develop")
	if _, err := runCtx(checkoutBranch(repos), tk, &tk.Branches[0], 0); err != nil {
		t.Fatal(err)
	}
	if head := mustGit(t, clone, "branch", "--show-current"); head != "feature/remote" {
		t.Errorf("head %q", head)
	}
}

func TestCheckoutRefusesDirtyAndUnmapped(t *testing.T) {
	_, clone, repos := gitFixture(t)
	os.WriteFile(filepath.Join(clone, "f"), []byte("x"), 0o600)
	mustGit(t, clone, "add", "f")
	tk := &Ticket{Key: "A-1", Branches: []Branch{{Name: "feature/remote", Repo: "ws/app"}, {Name: "x", Repo: "ws/other"}}}
	if _, err := runCtx(checkoutBranch(repos), tk, nil, 0); err == nil || err.Error() != "repo dirty: ws/app" {
		t.Errorf("dirty: %v", err)
	}
	if head := mustGit(t, clone, "branch", "--show-current"); head != "develop" {
		t.Errorf("dirty repo switched to %q", head)
	}
	if _, err := runCtx(checkoutBranch(repos), tk, nil, 1); err == nil || err.Error() != "repo not mapped: ws/other" {
		t.Errorf("unmapped: %v", err)
	}
}

func TestCheckoutUnknownBranchShowsGitError(t *testing.T) {
	_, _, repos := gitFixture(t)
	tk := &Ticket{Key: "A-1", Branches: []Branch{{Name: "gone", Repo: "ws/app"}}}
	if _, err := runCtx(checkoutBranch(repos), tk, nil, 0); err == nil || !strings.HasPrefix(err.Error(), "git fetch:") {
		t.Errorf("err %v", err)
	}
}

func TestBranchName(t *testing.T) {
	for _, c := range []struct {
		t    Ticket
		want string
	}{
		{Ticket{Key: "P-41", Type: "Bug", Summary: "List view – screen size issue"}, "bugfix/P-41-list-view-screen-size-issue"},
		{Ticket{Key: "P-1", Type: "Story", Summary: "Add a really long summary that keeps going well past the fifty character cap"}, "feature/P-1-add-a-really-long-summary-that-keeps-going-well-pa"},
		{Ticket{Key: "P-2", Summary: "  Café #12!  "}, "feature/P-2-caf-12"},
		{Ticket{Key: "P-3", Summary: "–––"}, "feature/P-3"},
	} {
		if got := branchName(&c.t); got != c.want {
			t.Errorf("%q: got %q want %q", c.t.Summary, got, c.want)
		}
	}
}

func TestNewBranchFromOriginBase(t *testing.T) {
	origin, clone, repos := gitFixture(t)
	mustGit(t, origin, "commit", "-q", "--allow-empty", "-m", "newer develop")
	var copied string
	a := newBranch(repos, func(s string) error { copied = s; return nil })
	tk := &Ticket{Key: "A-1", Type: "Bug", Summary: "Login fails"}
	title, opts, err := a.Choose(tk, nil)
	if err != nil || len(opts) != 1 || opts[0] != "ws/app" || title != "Repo for bugfix/A-1-login-fails" {
		t.Fatalf("choose %q %v %v", title, opts, err)
	}
	if _, err := runCtx(a, tk, nil, 0); err != nil {
		t.Fatal(err)
	}
	if head := mustGit(t, clone, "branch", "--show-current"); head != "bugfix/A-1-login-fails" || copied != head {
		t.Errorf("head %q, copied %q", head, copied)
	}
	if got, want := mustGit(t, clone, "rev-parse", "HEAD"), mustGit(t, origin, "rev-parse", "develop"); got != want {
		t.Error("new branch should start at the freshly fetched origin/develop")
	}
	if _, err := runGit(clone, "rev-parse", "--abbrev-ref", "@{upstream}"); err == nil {
		t.Error("new branch should not track origin/develop")
	}
}

func TestNewBranchNeedsMappedRepo(t *testing.T) {
	if _, _, err := newBranch(nil, nil).Choose(&Ticket{Key: "A-1"}, nil); err == nil || !strings.Contains(err.Error(), "no repos mapped") {
		t.Errorf("err %v", err)
	}
}

func TestCleanupDeletesOnlyMergedLocal(t *testing.T) {
	_, clone, repos := gitFixture(t)
	mustGit(t, clone, "branch", "merged-one")
	mustGit(t, clone, "checkout", "-q", "-b", "unmerged-one")
	mustGit(t, clone, "commit", "-q", "--allow-empty", "-m", "squash leftovers")
	mustGit(t, clone, "checkout", "-q", "develop")
	merged := []PR{{Status: "MERGED", Target: "develop"}}
	tk := &Ticket{Key: "A-1", Branches: []Branch{
		{Name: "merged-one", Repo: "ws/app", PRs: merged},
		{Name: "unmerged-one", Repo: "ws/app", PRs: merged},
		{Name: "open-one", Repo: "ws/app", PRs: []PR{{Status: "OPEN"}}},
		{Name: "still-in-review", Repo: "ws/app", PRs: []PR{{Status: "MERGED"}, {Status: "OPEN"}}},
		{Name: "not-local", Repo: "ws/app", PRs: merged},
		{Name: "elsewhere", Repo: "ws/other", PRs: merged},
	}}
	mustGit(t, clone, "branch", "open-one")
	mustGit(t, clone, "branch", "still-in-review")
	a := cleanupMerged(repos)
	title, opts, err := a.Choose(tk, nil)
	if err != nil || title != "Delete local branches? ws/app merged-one, ws/app unmerged-one" || strings.Join(opts, ",") != "cancel,delete" {
		t.Fatalf("choose %q %v %v", title, opts, err)
	}

	if note, err := runCtx(a, tk, nil, 0); err != nil || note != "nothing deleted" {
		t.Errorf("cancel: %q %v", note, err)
	}
	if !localBranchExists(repos["ws/app"], "merged-one") {
		t.Fatal("cancel deleted a branch")
	}

	// A branch created after the confirm is not deleted: Run uses the confirmed list.
	mustGit(t, clone, "branch", "late-one")
	tk.Branches = append(tk.Branches, Branch{Name: "late-one", Repo: "ws/app", PRs: merged})
	note, err := runCtx(a, tk, nil, 1)
	if err != nil || !strings.HasPrefix(note, "deleted merged-one; kept unmerged-one (git branch:") || strings.Contains(note, "\n") {
		t.Errorf("note %q, err %v", note, err)
	}
	for name, want := range map[string]bool{"merged-one": false, "unmerged-one": true, "open-one": true, "still-in-review": true, "late-one": true} {
		if localBranchExists(repos["ws/app"], name) != want {
			t.Errorf("%s exists = %v", name, !want)
		}
	}
	if _, _, err := a.Choose(&Ticket{Key: "B-2"}, nil); err == nil || err.Error() != "no merged local branches for B-2" {
		t.Errorf("empty: %v", err)
	}
	unmapped := &Ticket{Key: "C-3", Branches: []Branch{{Name: "x", Repo: "ws/other", PRs: merged}}}
	if _, _, err := a.Choose(unmapped, nil); err == nil || err.Error() != "repo not mapped: ws/other" {
		t.Errorf("unmapped: %v", err)
	}
}

func TestCleanupAllKeptIsAnError(t *testing.T) {
	_, clone, repos := gitFixture(t)
	mustGit(t, clone, "checkout", "-q", "-b", "unmerged-one")
	mustGit(t, clone, "commit", "-q", "--allow-empty", "-m", "x")
	mustGit(t, clone, "checkout", "-q", "develop")
	tk := &Ticket{Key: "A-1", Branches: []Branch{{Name: "unmerged-one", Repo: "ws/app", PRs: []PR{{Status: "MERGED"}}}}}
	a := cleanupMerged(repos)
	if _, _, err := a.Choose(tk, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := runCtx(a, tk, nil, 1); err == nil || !strings.HasPrefix(err.Error(), "git kept unmerged-one") {
		t.Errorf("err %v", err)
	}
}

func TestCheckoutRejectsOptionLikeName(t *testing.T) {
	_, clone, repos := gitFixture(t)
	marker := filepath.Join(t.TempDir(), "ran")
	tk := &Ticket{Key: "A-1", Branches: []Branch{{Name: "--upload-pack=touch " + marker, Repo: "ws/app"}}}
	if _, err := runCtx(checkoutBranch(repos), tk, nil, 0); err == nil || !strings.HasPrefix(err.Error(), "invalid branch name") {
		t.Errorf("err %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("injected command ran")
	}
	if head := mustGit(t, clone, "branch", "--show-current"); head != "develop" {
		t.Errorf("head %q", head)
	}
}

func TestCheckoutInSingleBranchClone(t *testing.T) {
	origin, _, _ := gitFixture(t)
	single := filepath.Join(t.TempDir(), "single")
	mustGit(t, origin, "clone", "-q", "--single-branch", "-b", "develop", origin, single)
	tk := &Ticket{Key: "A-1", Branches: []Branch{{Name: "feature/remote", Repo: "ws/app"}}}
	if _, err := runCtx(checkoutBranch(map[string]Repo{"ws/app": {Path: single, Base: "develop"}}), tk, nil, 0); err != nil {
		t.Fatal(err)
	}
	if head := mustGit(t, single, "branch", "--show-current"); head != "feature/remote" {
		t.Errorf("head %q", head)
	}
	// pull/push read these; rev-parse @{upstream} can't resolve it outside the fetch refspec.
	if r, m := mustGit(t, single, "config", "branch.feature/remote.remote"), mustGit(t, single, "config", "branch.feature/remote.merge"); r != "origin" || m != "refs/heads/feature/remote" {
		t.Errorf("upstream config %q %q", r, m)
	}
}

func TestGitNeverPrompts(t *testing.T) {
	_, clone, _ := gitFixture(t)
	t.Setenv("GIT_SSH_COMMAND", "")
	env := mustGit(t, clone, "-c", "alias.showenv=!env", "showenv")
	for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes"} {
		if !strings.Contains(env, want) {
			t.Errorf("missing %s", want)
		}
	}
	t.Setenv("GIT_SSH_COMMAND", "my-ssh")
	if env := mustGit(t, clone, "-c", "alias.showenv=!env", "showenv"); strings.Contains(env, "BatchMode") {
		t.Error("user's GIT_SSH_COMMAND should be kept")
	}
}

func TestNewBranchRefusesDirtyAndExisting(t *testing.T) {
	_, clone, repos := gitFixture(t)
	a := newBranch(repos, func(string) error { return nil })
	tk := &Ticket{Key: "A-1", Summary: "x"}
	os.WriteFile(filepath.Join(clone, "f"), []byte("x"), 0o600)
	mustGit(t, clone, "add", "f")
	if _, err := runCtx(a, tk, nil, 0); err == nil || err.Error() != "repo dirty: ws/app" {
		t.Errorf("dirty: %v", err)
	}
	mustGit(t, clone, "reset", "-q", "--hard")
	mustGit(t, clone, "branch", "feature/A-1-x")
	if _, err := runCtx(a, tk, nil, 0); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("existing: %v", err)
	}
}

func TestNewBranchPicksChosenRepo(t *testing.T) {
	_, clone, repos := gitFixture(t)
	repos["ws/aaa"] = Repo{Path: "/nonexistent", Base: "develop"}
	a := newBranch(repos, func(string) error { return nil })
	tk := &Ticket{Key: "A-1", Summary: "x"}
	if _, opts, _ := a.Choose(tk, nil); strings.Join(opts, ",") != "ws/aaa,ws/app" {
		t.Fatalf("options %v", opts)
	}
	if _, err := runCtx(a, tk, nil, 1); err != nil {
		t.Fatal(err)
	}
	if head := mustGit(t, clone, "branch", "--show-current"); head != "feature/A-1-x" {
		t.Errorf("head %q", head)
	}
}
