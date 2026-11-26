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
  "keymap": { "copy-branch": "y", "copy-key": "Y", "show-detail": "s", "open-ticket": "o", "open-bitbucket": "b" }
}
```

## Tests

```sh
go test ./...
```

## Keys

`j/k` or arrows move, `g/G` top/bottom, `enter`/`space` fold or unfold a group, `h/l` fold/unfold the current group, `/` filter (Esc clears), `r` refresh, `y` copy branch, `Y` copy ticket key, `s` show description and comments in a popup (`j/k` scroll, `c` fold/unfold comments, `esc` close), `o` open ticket in browser, `b` open the branch's PR (open, else merged) or branch page in Bitbucket, `?` help, `q` quit.

## Adding a feature

Write an `Action` (`actions.go`) and append it to the list in `main.go`. The footer, help overlay and keymap pick it up automatically. An action that needs a choice sets `Choose`: one option runs directly, more open a picker, and `Run` gets the index as `ctx.Choice`. Actions run in the background; a second one is refused until the first finishes.

Branches come from Jira's undocumented dev-status endpoint (`BranchSource` in `jira.go`); swap that function if it breaks. Merge info comes from the same endpoint's pull requests (`PRSource`), so a branch merged without a pull request shows as not merged.
