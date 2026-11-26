package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// send feeds msg to the model and runs resulting commands synchronously.
func send(m tea.Model, msg tea.Msg) tea.Model {
	for msg != nil {
		var cmd tea.Cmd
		m, cmd = m.Update(msg)
		if cmd == nil {
			break
		}
		msg = cmd()
	}
	return m
}

func press(m tea.Model, keys ...string) tea.Model {
	special := map[string]tea.KeyType{"esc": tea.KeyEsc, "enter": tea.KeyEnter, "space": tea.KeySpace, "left": tea.KeyLeft}
	for _, k := range keys {
		if t, ok := special[k]; ok {
			m = send(m, tea.KeyMsg{Type: t})
		} else {
			m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		}
	}
	return m
}

func setup(t *testing.T) (tea.Model, *[]string) {
	copied := &[]string{}
	copy := func(s string) error { *copied = append(*copied, s); return nil }
	m := newModel(func() ([]Ticket, error) { return fixtureTickets(), nil }, []Action{copyBranch(copy)})
	return send(m, m.Init()()), copied
}

func TestAppRendersTree(t *testing.T) {
	m, _ := setup(t)
	out := m.View()
	for _, s := range []string{"▾ In Progress (1)", "▾ Blocked (1)", "A-1", "[Blocked]", "feature/A-1-login", "no branch"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in\n%s", s, out)
		}
	}
}

func TestAppCopiesBranchUnderCursor(t *testing.T) {
	m, copied := setup(t)
	m = press(m, "j", "j", "j", "y")
	if len(*copied) != 1 || (*copied)[0] != "A-1-api" {
		t.Errorf("copied %v", *copied)
	}
	if !strings.Contains(m.View(), "copied A-1-api") {
		t.Errorf("status missing:\n%s", m.View())
	}
}

func TestAppLastRowNothingToCopy(t *testing.T) {
	m, copied := setup(t)
	m = press(m, "G", "y")
	if len(*copied) != 0 {
		t.Errorf("copied %v", *copied)
	}
	if !strings.Contains(m.View(), "B-2: nothing to copy") {
		t.Errorf("status missing:\n%s", m.View())
	}
}

func TestAppFilterAndClear(t *testing.T) {
	m, _ := setup(t)
	m = press(m, "/", "c", "a", "r", "t")
	if strings.Contains(m.View(), "A-1") {
		t.Errorf("filter kept A-1:\n%s", m.View())
	}
	m = press(m, "esc")
	if !strings.Contains(m.View(), "A-1") {
		t.Errorf("clear lost A-1:\n%s", m.View())
	}
}

func TestAppShowsLoadError(t *testing.T) {
	m := newModel(func() ([]Ticket, error) { return nil, errors.New("Jira rejected credentials (401)") }, nil)
	out := send(m, m.Init()()).View()
	if !strings.Contains(out, "Jira rejected credentials (401)") {
		t.Errorf("error missing:\n%s", out)
	}
}

func TestAppFoldsGroups(t *testing.T) {
	m, _ := setup(t)
	m = press(m, "enter")
	if out := m.View(); strings.Contains(out, "A-1") || !strings.Contains(out, "▸ In Progress (1)") {
		t.Errorf("enter on title should fold:\n%s", out)
	}
	m = press(m, "space")
	if !strings.Contains(m.View(), "feature/A-1-login") {
		t.Errorf("space should unfold:\n%s", m.View())
	}
	// h on a branch row folds its group and puts the cursor on the title; y there does nothing.
	m = press(m, "j", "j", "h", "y")
	if out := m.View(); strings.Contains(out, "A-1") || strings.Contains(out, "nothing to copy") {
		t.Errorf("h should fold, y on title no-op:\n%s", out)
	}
	m = press(m, "l")
	if !strings.Contains(m.View(), "A-1-api") {
		t.Errorf("l should unfold:\n%s", m.View())
	}
}

func TestAppFoldSurvivesRefreshAndFilter(t *testing.T) {
	m, _ := setup(t)
	m = press(m, "enter", "r")
	if strings.Contains(m.View(), "A-1") {
		t.Errorf("fold lost on refresh:\n%s", m.View())
	}
	m = press(m, "/", "a", "p", "i", "enter")
	if !strings.Contains(m.View(), "A-1-api") {
		t.Errorf("filter should unfold:\n%s", m.View())
	}
	m = press(m, "esc")
	if strings.Contains(m.View(), "A-1") {
		t.Errorf("esc should restore fold:\n%s", m.View())
	}
}

func TestAppShowsMergeInfo(t *testing.T) {
	ts := fixtureTickets()
	ts[0].Branches[0].PRs = []PR{{Status: "MERGED", Target: "develop"}, {Status: "OPEN", Target: "main"}}
	ts[0].PRError = "pr boom"
	m := newModel(func() ([]Ticket, error) { return ts, nil }, nil)
	out := send(m, m.Init()()).View()
	for _, s := range []string{"feature/A-1-login  merged → develop, PR open → main", "pull request lookup failed for A-1: pr boom"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in\n%s", s, out)
		}
	}
}

func setupActions(t *testing.T, ts []Ticket) (tea.Model, *[]string) {
	got := &[]string{}
	rec := func(s string) error { *got = append(*got, s); return nil }
	actions := []Action{
		openBitbucket(rec),
		copyURL("https://x.atlassian.net", rec),
		copyBranch(rec),
	}
	m := newModel(func() ([]Ticket, error) { return ts, nil }, actions)
	return send(m, m.Init()()), got
}

func TestAppPickerChoosesBranchToOpen(t *testing.T) {
	m, opened := setupActions(t, fixtureTickets())
	m = press(m, "j", "b")
	if out := m.View(); !strings.Contains(out, "Branch for A-1") || !strings.Contains(out, "> web feature/A-1-login") {
		t.Fatalf("picker missing:\n%s", out)
	}
	m = press(m, "j", "enter")
	if len(*opened) != 1 || (*opened)[0] != "https://bitbucket.org/api/branch/A-1-api" {
		t.Errorf("opened %v", *opened)
	}
	if !strings.Contains(m.View(), "opened https://bitbucket.org/api/branch/A-1-api") {
		t.Errorf("status missing:\n%s", m.View())
	}
}

func TestAppPickerCancels(t *testing.T) {
	for _, key := range []string{"esc", "q"} {
		m, opened := setupActions(t, fixtureTickets())
		m = press(m, "j", "b", key)
		if out := m.View(); len(*opened) != 0 || strings.Contains(out, "Branch for") || !strings.Contains(out, "A-1") {
			t.Errorf("%s should cancel and keep the app running: %v\n%s", key, *opened, out)
		}
	}
}

func TestAppPickerMovesUp(t *testing.T) {
	m, opened := setupActions(t, fixtureTickets())
	press(m, "j", "b", "j", "k", "enter")
	if len(*opened) != 1 || (*opened)[0] != "https://bitbucket.org/web/branch/feature/A-1-login" {
		t.Errorf("opened %v", *opened)
	}
}

func TestAppFooterFitsEightyColumns(t *testing.T) {
	m, _ := setupActions(t, fixtureTickets())
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if out := m.View(); !strings.Contains(out, "b Y y · enter fold") || !strings.Contains(out, "? help · q quit") {
		t.Errorf("footer cut:\n%s", out)
	}
}

func TestAppClearsStaleBusyRefusal(t *testing.T) {
	m, _ := setupActions(t, fixtureTickets())
	mm := press(m, "j").(model)
	mm.busy, mm.status = true, busyStatus
	if out := send(mm, doneMsg{}).View(); strings.Contains(out, "busy:") {
		t.Errorf("busy refusal should clear:\n%s", out)
	}
	mm.status = statusMsg{"keep me", true}
	if out := send(mm, doneMsg{}).View(); !strings.Contains(out, "keep me") {
		t.Errorf("other status should stay:\n%s", out)
	}
}

func TestAppOpenBranchRowSkipsPicker(t *testing.T) {
	ts := fixtureTickets()
	ts[0].Branches[0].PRs = []PR{{Status: "OPEN", URL: "https://bb/pr/1"}}
	m, opened := setupActions(t, ts)
	press(m, "j", "j", "b")
	if len(*opened) != 1 || (*opened)[0] != "https://bb/pr/1" {
		t.Errorf("opened %v", *opened)
	}
}

func TestAppOpenNoBranch(t *testing.T) {
	m, _ := setupActions(t, fixtureTickets())
	m = press(m, "G", "b")
	if !strings.Contains(m.View(), "open in Bitbucket failed: B-2 has no branch") {
		t.Errorf("status missing:\n%s", m.View())
	}
}

func TestAppCopiesTicketURL(t *testing.T) {
	m, copied := setupActions(t, fixtureTickets())
	press(m, "j", "Y")
	if strings.Join(*copied, "|") != "https://x.atlassian.net/browse/A-1" {
		t.Errorf("copied %v", *copied)
	}
}

func TestAppRefusesActionWhileBusy(t *testing.T) {
	m, copied := setupActions(t, fixtureTickets())
	mm := m.(model)
	mm.busy = true
	out := press(mm, "j", "Y").View()
	if len(*copied) != 0 || !strings.Contains(out, "busy: wait for the current action") {
		t.Errorf("copied %v\n%s", *copied, out)
	}
}

func TestAppShowsWorkingUntilActionEnds(t *testing.T) {
	m, copied := setupActions(t, fixtureTickets())
	m = press(m, "j")
	// Update without running the command, so the action is still in flight.
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Y")})
	if !strings.Contains(m.View(), "working…") {
		t.Errorf("working status missing:\n%s", m.View())
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if !strings.Contains(m.View(), "busy: wait for the current action") {
		t.Errorf("second action not refused:\n%s", m.View())
	}
	m = send(m, cmd())
	out := m.View()
	if strings.Join(*copied, "|") != "https://x.atlassian.net/browse/A-1" || !strings.Contains(out, "copied https://x.atlassian.net/browse/A-1") || strings.Contains(out, "working…") {
		t.Errorf("copied %v\n%s", *copied, out)
	}
	if press(m, "y"); strings.Join(*copied, "|") != "https://x.atlassian.net/browse/A-1|feature/A-1-login" {
		t.Errorf("busy not cleared: %v", *copied)
	}
}

func TestAppPickerScrollsWithCursor(t *testing.T) {
	ts := fixtureTickets()
	for i := range 20 {
		ts[0].Branches = append(ts[0].Branches, Branch{Name: fmt.Sprintf("extra-%02d", i), Repo: "web"})
	}
	m, _ := setupActions(t, ts)
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 10})
	m = press(m, "j", "b")
	if out := m.View(); strings.Contains(out, "extra-19") || !strings.Contains(out, "esc/q cancel") {
		t.Errorf("picker should fit the screen:\n%s", out)
	}
	for range 21 {
		m = press(m, "j")
	}
	if out := m.View(); !strings.Contains(out, "> web extra-19") || strings.Contains(out, "feature/A-1-login") || strings.Contains(out, "▾") {
		t.Errorf("picker should scroll to the cursor:\n%s", out)
	}
}

func TestBitbucketURLOrder(t *testing.T) {
	b := &Branch{Name: "x", Repo: "r", URL: "https://b/branch", PRs: []PR{{Status: "MERGED", URL: "https://b/merged"}, {Status: "OPEN", URL: "https://b/open"}}}
	for _, want := range []string{"https://b/open", "https://b/merged", "https://b/branch", "https://bitbucket.org/r/branch/x"} {
		if got, _ := bitbucketURL(b); got != want {
			t.Errorf("got %q want %q", got, want)
		}
		switch want {
		case "https://b/open":
			b.PRs = b.PRs[:1]
		case "https://b/merged":
			b.PRs = nil
		case "https://b/branch":
			b.URL = ""
		}
	}
	if _, err := bitbucketURL(&Branch{Name: "x", Repo: "?"}); err == nil {
		t.Error("unknown repo should fail")
	}
	if got, _ := bitbucketURL(&Branch{Name: "x", Repo: "r", URL: "http://bb.local/x"}); got != "http://bb.local/x" {
		t.Errorf("http link should be kept: %q", got)
	}
	b = &Branch{Name: "bugfix/fix#12?x", Repo: "r", URL: "file:///etc/passwd", PRs: []PR{{Status: "OPEN", URL: `C:\x.exe`}}}
	if got, _ := bitbucketURL(b); got != "https://bitbucket.org/r/branch/bugfix/fix%2312%3Fx" {
		t.Errorf("non-https links should be skipped and the name escaped: %q", got)
	}
}

func TestAppDetailPopupFoldsComments(t *testing.T) {
	d := Detail{Key: "A-1", Summary: "Login", Description: "the description",
		Comments: []Comment{{Author: "Bob", Created: "2026-11-20T10:05:00.000+0000", Body: "looks good"}}}
	var asked string
	fetch := func(key string) (Detail, error) { asked = key; return d, nil }
	m := newModel(func() ([]Ticket, error) { return fixtureTickets(), nil }, []Action{showDetail(fetch)})
	m2 := press(send(m, m.Init()()), "j", "s")
	out := m2.View()
	if asked != "A-1" || !strings.Contains(out, "the description") || !strings.Contains(out, "▸ Comments (1)") || strings.Contains(out, "looks good") {
		t.Fatalf("asked %q, popup:\n%s", asked, out)
	}
	out = press(m2, "c").View()
	if !strings.Contains(out, "▾ Comments (1)") || !strings.Contains(out, "Bob 2026-11-20 10:05") || !strings.Contains(out, "looks good") {
		t.Errorf("comments not shown:\n%s", out)
	}
	if out := press(m2, "esc").View(); strings.Contains(out, "the description") {
		t.Errorf("popup not closed:\n%s", out)
	}
}

func TestAppDetailPopupScrollsAndSurvivesResize(t *testing.T) {
	d := Detail{Key: "A-1", Description: strings.Repeat("lorem ipsum dolor ", 60) + "THE-END"}
	m := newModel(func() ([]Ticket, error) { return fixtureTickets(), nil }, []Action{showDetail(func(string) (Detail, error) { return d, nil })})
	m2 := send(send(m, m.Init()()), tea.WindowSizeMsg{Width: 30, Height: 10})
	m2 = press(m2, "j", "s", "G")
	if out := m2.View(); !strings.Contains(out, "THE-END") || !strings.Contains(out, "▸ Comments (0)") {
		t.Errorf("G did not reach the end:\n%s", out)
	}
	m2 = send(m2, tea.WindowSizeMsg{Width: 140, Height: 10})
	if out := m2.View(); !strings.Contains(out, "THE-END") {
		t.Errorf("after resize:\n%s", out)
	}
}

func TestAppDetailFetchErrorShowsStatus(t *testing.T) {
	m := newModel(func() ([]Ticket, error) { return fixtureTickets(), nil }, []Action{showDetail(func(string) (Detail, error) { return Detail{}, errors.New("Jira 404") })})
	out := press(send(m, m.Init()()), "j", "s").View()
	if !strings.Contains(out, "show description and comments failed: Jira 404") || strings.Contains(out, "Comments (") {
		t.Errorf("error not shown:\n%s", out)
	}
}

func setupTicketActions(t *testing.T, ts []Ticket) (tea.Model, *[]string) {
	got := &[]string{}
	rec := func(s string) error { *got = append(*got, s); return nil }
	list := func(key string) ([]Transition, error) {
		return []Transition{{ID: "11", Name: "Review", To: "In Review"}, {ID: "21", Name: "Reopen", To: "To Do"},
			{ID: "22", Name: "Back", To: "To Do"}, {ID: "31", Name: "Close", To: "Done", NeedsFields: true}}, nil
	}
	apply := func(key, id string) error { *got = append(*got, "transition "+key+" "+id); return nil }
	users := func(key string) ([]User, error) { return []User{{AccountID: "u1", Name: "Ann"}}, nil }
	me := func() (string, error) { return "me-id", nil }
	assign := func(key, id string) error { *got = append(*got, "assign "+key+" "+id); return nil }
	actions := []Action{
		transitionTicket("https://x.atlassian.net", list, apply, rec),
		assignTicket(users, me, assign),
		openPR([]string{"master", "uat", "qa"}, rec),
	}
	m := newModel(func() ([]Ticket, error) { return ts, nil }, actions)
	return send(m, m.Init()()), got
}

func TestAppTransitionUpdatesRow(t *testing.T) {
	m, got := setupTicketActions(t, fixtureTickets())
	m = press(m, "j", "t")
	out := m.View()
	for _, s := range []string{"Transition A-1 [In Progress]", "> In Review", "Reopen → To Do", "Back → To Do", "  Done"} {
		if !strings.Contains(out, s) {
			t.Fatalf("missing %q in\n%s", s, out)
		}
	}
	out = press(m, "enter").View()
	if strings.Join(*got, "|") != "transition A-1 11" || !strings.Contains(out, "A-1 → In Review") || !strings.Contains(out, "[In Review]") {
		t.Errorf("got %v\n%s", *got, out)
	}
}

func TestAppTransitionNeedingFieldsOpensBrowser(t *testing.T) {
	m, got := setupTicketActions(t, fixtureTickets())
	out := press(m, "j", "t", "G", "j", "j", "j", "enter").View()
	if strings.Join(*got, "|") != "https://x.atlassian.net/browse/A-1" || !strings.Contains(out, `A-1: "Close" needs fields, opened in browser`) || !strings.Contains(out, "[In Progress]") {
		t.Errorf("got %v\n%s", *got, out)
	}
}

func TestAppAssign(t *testing.T) {
	for _, c := range []struct {
		keys       []string
		want, note string
	}{
		{nil, "assign A-1 me-id", "A-1 → me"},
		{[]string{"j"}, "assign A-1 ", "A-1 unassigned"},
		{[]string{"j", "j"}, "assign A-1 u1", "A-1 → Ann"},
	} {
		m, got := setupTicketActions(t, fixtureTickets())
		m = press(m, "j", "a")
		if out := m.View(); !strings.Contains(out, "Assign A-1") || !strings.Contains(out, "Ann") {
			t.Fatalf("picker missing:\n%s", out)
		}
		out := press(m, append(c.keys, "enter")...).View()
		if strings.Join(*got, "|") != c.want || !strings.Contains(out, c.note) {
			t.Errorf("got %v\n%s", *got, out)
		}
	}
}

func TestAppOpenPRChainsBranchAndTargetPickers(t *testing.T) {
	m, got := setupTicketActions(t, fixtureTickets())
	m = press(m, "j", "p")
	if !strings.Contains(m.View(), "Branch for A-1") {
		t.Fatalf("branch picker missing:\n%s", m.View())
	}
	m = press(m, "enter")
	if out := m.View(); !strings.Contains(out, "Target branch for feature/A-1-login") || !strings.Contains(out, "Other…") {
		t.Fatalf("target picker missing:\n%s", out)
	}
	out := press(m, "j", "enter").View()
	want := "https://bitbucket.org/web/pull-requests/new?dest=uat&source=feature%2FA-1-login"
	if strings.Join(*got, "|") != want || !strings.Contains(out, "opened "+want) {
		t.Errorf("got %v\n%s", *got, out)
	}
}

func TestAppOpenPRTypedTarget(t *testing.T) {
	m, got := setupTicketActions(t, fixtureTickets())
	m = press(m, "j", "j", "p", "G", "j", "j", "j", "enter")
	if !strings.Contains(m.View(), "type a name") {
		t.Fatalf("text input missing:\n%s", m.View())
	}
	m = press(m, "enter") // empty: stays open
	m = press(m, "space", "r", "e", "l", "x")
	m = send(m, tea.KeyMsg{Type: tea.KeyBackspace})
	press(m, "/", "1", "enter")
	if strings.Join(*got, "|") != "https://bitbucket.org/web/pull-requests/new?dest=rel%2F1&source=feature%2FA-1-login" {
		t.Errorf("got %v", *got)
	}
	m, got = setupTicketActions(t, fixtureTickets())
	out := press(m, "j", "j", "p", "k", "k", "k", "j", "j", "j", "enter", "q", "esc").View()
	if len(*got) != 0 || strings.Contains(out, "Target branch") {
		t.Errorf("esc should cancel typing: %v\n%s", *got, out)
	}
}

func TestAppOpenPRPrefersOpenPR(t *testing.T) {
	ts := fixtureTickets()
	ts[0].Branches[0].PRs = []PR{{Status: "OPEN", Target: "qa", URL: "https://bb/pr/9"}, {Status: "MERGED", Target: "master", URL: "https://bb/pr/1"}}
	m, got := setupTicketActions(t, ts)
	press(m, "j", "j", "p", "j", "j", "enter")
	if strings.Join(*got, "|") != "https://bb/pr/9" {
		t.Errorf("got %v", *got)
	}
	m, got = setupTicketActions(t, ts)
	press(m, "j", "j", "p", "enter")
	if !strings.Contains(strings.Join(*got, "|"), "/pull-requests/new?dest=master") {
		t.Errorf("merged PR should not be reused: %v", *got)
	}
}

func TestWithKeymapRejectsBadBindings(t *testing.T) {
	as := []Action{copyBranch(nil), copyURL("", nil)}
	for keymap, want := range map[string]string{
		`{"copy-brnach":"c"}`:                 `unknown action "copy-brnach" (known: copy-branch, copy-url)`,
		`{"copy-branch":"j"}`:                 `copy-branch: "j" is a navigation key`,
		`{"copy-branch":"Y"}`:                 `copy-branch and copy-url are both bound to "Y"`,
		`{"copy-branch":"",  "copy-url":"y"}`: `copy-branch and copy-url are both bound to "y"`,
	} {
		var km map[string]string
		json.Unmarshal([]byte(keymap), &km)
		if _, err := withKeymap(as, km); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", keymap, err)
		}
	}
}
