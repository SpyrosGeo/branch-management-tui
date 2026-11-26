package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fixtureTickets() []Ticket {
	return []Ticket{
		{ID: "1", Key: "A-1", Summary: "Login page", Status: "In Progress", Branches: []Branch{{Name: "feature/A-1-login", Repo: "web"}, {Name: "A-1-api", Repo: "api"}}},
		{ID: "2", Key: "B-2", Summary: "Cart", Status: "Blocked"},
	}
}

func labels(rows []Row) []string {
	var out []string
	for _, r := range rows {
		switch {
		case r.Ticket == nil:
			out = append(out, fmt.Sprintf("#%s(%d)", r.Group, r.Count))
		case r.Branch != nil:
			out = append(out, r.Branch.Name)
		default:
			out = append(out, r.Ticket.Key)
		}
	}
	return out
}

func TestBuildRows(t *testing.T) {
	ts := fixtureTickets()
	none := map[string]bool{}
	if got := labels(buildRows(ts, "", none)); !reflect.DeepEqual(got, []string{"#In Progress(1)", "A-1", "feature/A-1-login", "A-1-api", "#Blocked(1)", "B-2"}) {
		t.Errorf("nesting: %v", got)
	}
	if got := labels(buildRows(ts, "API", none)); !reflect.DeepEqual(got, []string{"#In Progress(1)", "A-1", "A-1-api"}) {
		t.Errorf("branch filter: %v", got)
	}
	if got := buildRows(ts, "login page", none); len(got) != 4 {
		t.Errorf("ticket match should keep all branches: %v", labels(got))
	}
	if got := buildRows(ts, "zzz", none); len(got) != 0 {
		t.Errorf("no match: %v", labels(got))
	}
}

func TestBuildRowsGroupOrder(t *testing.T) {
	var ts []Ticket
	for i, s := range []string{"To Do", "Blocked", "Issue Found", "In Progress", "To Do", "Code Review"} {
		ts = append(ts, Ticket{Key: fmt.Sprintf("K-%d", i), Status: s})
	}
	var groups []string
	for _, r := range buildRows(ts, "", map[string]bool{}) {
		if r.Ticket == nil {
			groups = append(groups, fmt.Sprintf("%s(%d)", r.Group, r.Count))
		}
	}
	want := []string{"In Progress(1)", "Issue Found(1)", "Blocked(1)", "Code Review(1)", "To Do(2)"}
	if !reflect.DeepEqual(groups, want) {
		t.Errorf("order %v", groups)
	}
}

func TestBuildRowsFolding(t *testing.T) {
	ts := fixtureTickets()
	folded := map[string]bool{"In Progress": true}
	if got := labels(buildRows(ts, "", folded)); !reflect.DeepEqual(got, []string{"#In Progress(1)", "#Blocked(1)", "B-2"}) {
		t.Errorf("folded: %v", got)
	}
	if !buildRows(ts, "", folded)[0].Folded {
		t.Error("title should report folded")
	}
	// A filter match unfolds the group without forgetting the fold.
	if got := labels(buildRows(ts, "api", folded)); !reflect.DeepEqual(got, []string{"#In Progress(1)", "A-1", "A-1-api"}) {
		t.Errorf("filter should unfold: %v", got)
	}
	if !folded["In Progress"] {
		t.Error("fold state lost")
	}
}

func TestCopyBranch(t *testing.T) {
	ts := fixtureTickets()
	var copied []string
	var last string
	a := copyBranch(func(s string) error { copied = append(copied, s); return nil })
	notify := func(s string) { last = s }
	for _, c := range []ActionContext{
		{Ticket: &ts[0], Branch: &ts[0].Branches[1], Notify: notify},
		{Ticket: &ts[0], Notify: notify},
		{Ticket: &ts[1], Notify: notify},
	} {
		if err := a.Run(c); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(copied, []string{"A-1-api", "feature/A-1-login"}) {
		t.Errorf("copied %v", copied)
	}
	if last != "B-2: nothing to copy" {
		t.Errorf("last notify %q", last)
	}
	if k := withKeymap([]Action{a}, map[string]string{"copy-branch": "c"})[0].Key; k != "c" {
		t.Errorf("keymap: %q", k)
	}
	failing := copyBranch(func(string) error { return errors.New("x") })
	if err := failing.Run(ActionContext{Ticket: &ts[0], Notify: notify}); err == nil {
		t.Error("copy error should propagate")
	}
}

func TestOpenTicket(t *testing.T) {
	ts := fixtureTickets()
	var opened, note string
	a := openTicket("https://x.atlassian.net", func(u string) error { opened = u; return nil })
	if err := a.Run(ActionContext{Ticket: &ts[0], Branch: &ts[0].Branches[1], Notify: func(s string) { note = s }}); err != nil {
		t.Fatal(err)
	}
	if opened != "https://x.atlassian.net/browse/A-1" || note != "opened A-1" {
		t.Errorf("opened %q, note %q", opened, note)
	}
}

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	_, err := loadConfig(envOf(nil), filepath.Join(dir, "absent.json"))
	if err == nil || !strings.Contains(err.Error(), "JIRA_BASE_URL") || !strings.Contains(err.Error(), "JIRA_EMAIL") || !strings.Contains(err.Error(), "JIRA_API_TOKEN") {
		t.Errorf("missing settings: %v", err)
	}

	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte(`{"baseUrl":"https://x.atlassian.net/","keymap":{"copy-branch":"c"}}`), 0o600)
	c, err := loadConfig(envOf(map[string]string{"JIRA_EMAIL": "e", "JIRA_API_TOKEN": "t"}), path)
	if err != nil {
		t.Fatal(err)
	}
	want := Config{BaseURL: "https://x.atlassian.net", Email: "e", Token: "t", JQL: DefaultJQL, Keymap: map[string]string{"copy-branch": "c"}}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("got %+v", c)
	}

	os.WriteFile(path, []byte(`{nope`), 0o600)
	if _, err := loadConfig(envOf(nil), path); err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("bad JSON: %v", err)
	}
}
