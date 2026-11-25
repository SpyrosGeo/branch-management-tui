package main

import (
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
		copyKey(rec),
		copySummary(rec),
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
	if out := m.View(); !strings.Contains(out, "b Y S · enter fold") || !strings.Contains(out, "? help · q quit") {
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

func TestAppCopiesKeyAndSummary(t *testing.T) {
	m, copied := setupActions(t, fixtureTickets())
	press(m, "j", "Y", "S")
	if strings.Join(*copied, "|") != "A-1|Login page" {
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
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	if !strings.Contains(m.View(), "busy: wait for the current action") {
		t.Errorf("second action not refused:\n%s", m.View())
	}
	m = send(m, cmd())
	out := m.View()
	if strings.Join(*copied, "|") != "A-1" || !strings.Contains(out, "copied A-1") || strings.Contains(out, "working…") {
		t.Errorf("copied %v\n%s", *copied, out)
	}
	if press(m, "S"); strings.Join(*copied, "|") != "A-1|Login page" {
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
