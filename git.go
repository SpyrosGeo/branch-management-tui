package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// shortcut: every repo's remote is assumed to be "origin"; add a "remote" repo setting if one isn't.
const remote = "origin"

// runGit runs git in dir and returns trimmed stdout; a failure carries git's own message.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	// Never prompt: a credential or passphrase prompt on /dev/tty would draw over the TUI and hang the action.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// First line only: the status bar is one row, and git's later lines are hints.
		msg, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n")
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return strings.TrimSpace(string(out)), nil
}

// checkBranchName rejects names git would not accept as a branch, such as "--upload-pack=…";
// names come from Jira and must not reach git as options.
func checkBranchName(dir, name string) error {
	if _, err := runGit(dir, "check-ref-format", "--branch", name); err != nil {
		return fmt.Errorf("invalid branch name %q", name)
	}
	return nil
}

// fetchBranch updates origin/<branch> with an explicit refspec, so it also works in single-branch clones.
func fetchBranch(dir, branch string) error {
	_, err := runGit(dir, "fetch", remote, "+refs/heads/"+branch+":refs/remotes/"+remote+"/"+branch)
	return err
}

// checkoutTracking creates branch from origin/<branch> and sets its upstream by config,
// since --track refuses refs outside the clone's fetch refspec (single-branch clones).
func checkoutTracking(dir, branch string) error {
	if _, err := runGit(dir, "checkout", "-b", branch, "--no-track", remote+"/"+branch); err != nil {
		return err
	}
	if _, err := runGit(dir, "config", "branch."+branch+".remote", remote); err != nil {
		return err
	}
	_, err := runGit(dir, "config", "branch."+branch+".merge", "refs/heads/"+branch)
	return err
}

func repoFor(repos map[string]Repo, name string) (Repo, error) {
	r, ok := repos[name]
	if !ok {
		return Repo{}, errors.New("repo not mapped: " + name)
	}
	return r, nil
}

// checkClean refuses a repo with uncommitted changes to tracked files; git itself refuses
// a checkout that would overwrite untracked ones.
func checkClean(name string, r Repo) error {
	out, err := runGit(r.Path, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return err
	}
	if out != "" {
		return errors.New("repo dirty: " + name)
	}
	return nil
}

func localBranchExists(r Repo, branch string) bool {
	_, err := runGit(r.Path, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

func checkoutBranch(repos map[string]Repo) Action {
	return Action{
		ID:      "checkout-branch",
		Key:     "c",
		Label:   "checkout",
		Applies: func(*Ticket, *Branch) bool { return true },
		Choose:  chooseBranch,
		Run: func(c ActionContext) error {
			b := rowBranches(c.Ticket, c.Branch)[c.Choice]
			r, err := repoFor(repos, b.Repo)
			if err != nil {
				return err
			}
			if err := checkBranchName(r.Path, b.Name); err != nil {
				return err
			}
			if err := checkClean(b.Repo, r); err != nil {
				return err
			}
			if localBranchExists(r, b.Name) {
				_, err = runGit(r.Path, "checkout", b.Name)
			} else if err = fetchBranch(r.Path, b.Name); err == nil {
				err = checkoutTracking(r.Path, b.Name)
			}
			if err != nil {
				return err
			}
			c.Notify(fmt.Sprintf("checked out %s in %s", b.Name, r.Path))
			return nil
		},
	}
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// branchName builds bugfix/<KEY>-<slug> for bugs and feature/<KEY>-<slug> otherwise.
func branchName(t *Ticket) string {
	prefix := "feature/"
	if t.Type == "Bug" {
		prefix = "bugfix/"
	}
	slug := nonSlug.ReplaceAllString(strings.ToLower(t.Summary), "-")
	slug = strings.Trim(slug[:min(len(slug), 50)], "-") // ASCII only, so byte slicing is safe
	if slug == "" {
		return prefix + t.Key
	}
	return prefix + t.Key + "-" + slug
}

func mappedRepos(repos map[string]Repo) []string {
	var names []string
	for n := range repos {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func newBranch(repos map[string]Repo, copy func(string) error) Action {
	return Action{
		ID:      "new-branch",
		Key:     "n",
		Label:   "new branch",
		Applies: func(*Ticket, *Branch) bool { return true },
		Choose: func(t *Ticket, _ *Branch) (string, []string, error) {
			names := mappedRepos(repos)
			if len(names) == 0 {
				return "", nil, errors.New(`no repos mapped; add "repos" to the config`)
			}
			return "Repo for " + branchName(t), names, nil
		},
		Run: func(c ActionContext) error {
			name := mappedRepos(repos)[c.Choice]
			r := repos[name]
			branch := branchName(c.Ticket)
			if err := checkBranchName(r.Path, branch); err != nil {
				return err
			}
			if err := checkClean(name, r); err != nil {
				return err
			}
			if err := fetchBranch(r.Path, r.Base); err != nil {
				return err
			}
			if _, err := runGit(r.Path, "checkout", "-b", branch, "--no-track", remote+"/"+r.Base); err != nil {
				return err
			}
			if err := copy(branch); err != nil {
				return err
			}
			c.Notify(fmt.Sprintf("created and copied %s in %s", branch, name))
			return nil
		},
	}
}

// mergedLocal lists the row's local branches whose PRs are merged and none still open, in mapped
// repos. unmapped names the repos it skipped, so an empty result can say why.
func mergedLocal(repos map[string]Repo, t *Ticket, b *Branch) (out []*Branch, unmapped []string) {
	for _, x := range rowBranches(t, b) {
		r, ok := repos[x.Repo]
		if !ok {
			unmapped = append(unmapped, x.Repo)
			continue
		}
		merged, open := false, false
		for _, p := range x.PRs {
			merged = merged || p.Status == "MERGED"
			open = open || p.Status == "OPEN"
		}
		if merged && !open && checkBranchName(r.Path, x.Name) == nil && localBranchExists(r, x.Name) {
			out = append(out, x)
		}
	}
	return out, unmapped
}

// cleanupMerged deletes local branches whose PR is merged. -d only deletes a branch whose commits
// are on its upstream (or on HEAD when it has none), so unpushed work is kept.
func cleanupMerged(repos map[string]Repo) Action {
	// confirmed is the list shown in the confirm, so Run deletes exactly that.
	// Safe to share: one action runs at a time, and the picker blocks other keys.
	var confirmed []*Branch
	return Action{
		ID:      "cleanup-merged",
		Key:     "D",
		Label:   "delete merged local branches",
		Applies: func(*Ticket, *Branch) bool { return true },
		Choose: func(t *Ticket, b *Branch) (string, []string, error) {
			bs, unmapped := mergedLocal(repos, t, b)
			confirmed = bs
			if len(bs) == 0 {
				if len(unmapped) > 0 {
					return "", nil, errors.New("repo not mapped: " + strings.Join(unmapped, ", "))
				}
				return "", nil, errors.New("no merged local branches for " + t.Key)
			}
			var names []string
			for _, x := range bs {
				names = append(names, x.Repo+" "+x.Name)
			}
			// Cancel comes first so a stray enter deletes nothing.
			return "Delete local branches? " + strings.Join(names, ", "), []string{"cancel", "delete"}, nil
		},
		Run: func(c ActionContext) error {
			if c.Choice != 1 {
				c.Notify("nothing deleted")
				return nil
			}
			var deleted, kept []string
			for _, b := range confirmed {
				if _, err := runGit(repos[b.Repo].Path, "branch", "-d", b.Name); err != nil {
					kept = append(kept, fmt.Sprintf("%s (%v)", b.Name, err))
					continue
				}
				deleted = append(deleted, b.Name)
			}
			msg := "deleted " + strings.Join(deleted, ", ")
			if len(kept) > 0 {
				msg += "; kept " + strings.Join(kept, "; ")
			}
			if len(deleted) == 0 {
				return errors.New("git kept " + strings.Join(kept, "; "))
			}
			c.Notify(msg)
			return nil
		},
	}
}
