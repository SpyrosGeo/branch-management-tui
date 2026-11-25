package main

import (
	"errors"
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
