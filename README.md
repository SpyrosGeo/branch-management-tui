# branch-tui

Keyboard-driven terminal UI listing your in-progress Jira tickets with their linked Bitbucket repos and branches.

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
  "jql": "assignee = currentUser() AND statusCategory = \"In Progress\" ORDER BY updated DESC",
  "keymap": { "copy-branch": "y" }
}
```

## Tests

```sh
go test ./...
```

## Keys

`j/k` or arrows move, `g/G` top/bottom, `/` filter (Esc clears), `r` refresh, `y` copy branch, `?` help, `q` quit.

## Adding a feature

Write an `Action` (`actions.go`) and append it to the list in `main.go`. The footer, help overlay and keymap pick it up automatically.

Branches come from Jira's undocumented dev-status endpoint (`BranchSource` in `jira.go`); swap that function if it breaks.
