# branch-tui

Keyboard-driven terminal UI listing your open (not Done) Jira tickets, grouped by status, with their linked Bitbucket repos and branches.

## Setup

```sh
go mod tidy && go install .   # installs the `branch-tui` command
```

Secrets come from the environment ([create an API token](https://id.atlassian.com/manage-profile/security/api-tokens)):

```sh
export JIRA_EMAIL=you@example.com
export JIRA_API_TOKEN=...
```

Non-secret settings live in `~/.config/branch-tui/config.json` (all optional except `baseUrl`, which can also be `JIRA_BASE_URL`):

```json
{
  "baseUrl": "https://your-site.atlassian.net",
  "jql": "assignee = currentUser() AND statusCategory != Done ORDER BY updated DESC",
  "keymap": { "copy-branch": "y", "copy-key": "Y", "copy-summary": "S", "open-ticket": "o", "open-bitbucket": "b",
              "checkout-branch": "c", "new-branch": "n", "cleanup-merged": "D" },
  "repos": {
    "pmmretail/mediamarkt": { "path": "~/code/mediamarkt", "base": "develop" }
  }
}
```

`repos` maps a repo's full name, as shown next to each branch, to its local clone. The git keys need it; `base` is the branch new branches start from (default `develop`). The remote is assumed to be `origin`. Git never prompts: fetches that need a password or a locked SSH key fail with git's message, so use a credential helper or ssh-agent.

## Tests

```sh
go test ./...
```

## Keys

`j/k` or arrows move, `g/G` top/bottom, `enter`/`space` fold or unfold a group, `h/l` fold/unfold the current group, `/` filter (Esc clears), `r` refresh, `y` copy branch, `Y` copy ticket key, `S` copy summary, `o` open ticket in browser, `b` open the branch's PR (open, else merged) or branch page in Bitbucket, `c` check out the branch locally (fetching it if needed), `n` create `feature/<KEY>-<summary>` (`bugfix/` for bugs) from `origin/<base>` and copy its name, `D` delete local branches whose PR is merged and none is still open (after a confirm; `git branch -d` only deletes a branch whose commits are on its upstream, or on HEAD when it has none, so unpushed work is kept), `?` help, `q` quit.

## Adding a feature

Write an `Action` (`actions.go`) and append it to the list in `main.go`. The footer, help overlay and keymap pick it up automatically. An action that needs a choice sets `Choose`: one option runs directly, more open a picker, and `Run` gets the index as `ctx.Choice`. Actions run in the background; a second one is refused until the first finishes.

Branches come from Jira's undocumented dev-status endpoint (`BranchSource` in `jira.go`); swap that function if it breaks. Merge info comes from the same endpoint's pull requests (`PRSource`), so a branch merged without a pull request shows as not merged.
