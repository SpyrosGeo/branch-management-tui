package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var navHelp = [][2]string{
	{"j/k ↑/↓", "move"},
	{"g/G", "top/bottom"},
	{"enter/space", "fold/unfold group"},
	{"h/l ←/→", "fold/unfold current group"},
	{"/", "filter (Esc clears)"},
	{"r", "refresh"},
	{"?", "help"},
	{"q", "quit"},
}

var (
	cyan     = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	keyStyle = cyan.Bold(true)
	magenta  = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	yellow   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	green    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	red      = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	blue     = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	grey     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	dim      = lipgloss.NewStyle().Faint(true)
	inverse  = lipgloss.NewStyle().Reverse(true)
	box      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)

func statusStyle(status string) lipgloss.Style {
	switch status {
	case "In Progress":
		return yellow
	case "Issue Found":
		return red
	case "Blocked":
		return magenta
	case "To Do":
		return blue
	}
	return grey
}

type loadedMsg struct {
	tickets []Ticket
	err     error
}

type statusMsg struct {
	text string
	err  bool
}

type model struct {
	load      func() ([]Ticket, error)
	actions   []Action
	tickets   []Ticket
	folded    map[string]bool // by status; kept across refreshes
	loading   bool
	status    statusMsg
	cursor    int
	filter    string
	filtering bool
	help      bool
	width     int
	height    int
}

func newModel(load func() ([]Ticket, error), actions []Action) model {
	return model{load: load, actions: actions, folded: map[string]bool{}, loading: true, height: 24}
}

func (m model) Init() tea.Cmd { return m.fetch() }

func (m model) fetch() tea.Cmd {
	return func() tea.Msg {
		t, err := m.load()
		return loadedMsg{t, err}
	}
}

func (m model) rows() []Row { return buildRows(m.tickets, m.filter, m.folded) }

func (m model) sel(n int) int { return max(0, min(m.cursor, n-1)) }

// lookupFailures names the tickets whose branch or pull request lookups failed.
func lookupFailures(tickets []Ticket) string {
	var msgs []string
	for _, kind := range []struct {
		what string
		err  func(Ticket) string
	}{
		{"branch", func(t Ticket) string { return t.BranchError }},
		{"pull request", func(t Ticket) string { return t.PRError }},
	} {
		var keys []string
		first := ""
		for _, t := range tickets {
			if e := kind.err(t); e != "" {
				keys = append(keys, t.Key)
				if first == "" {
					first = e
				}
			}
		}
		if len(keys) > 0 {
			msgs = append(msgs, fmt.Sprintf("%s lookup failed for %s: %s", kind.what, strings.Join(keys, ", "), first))
		}
	}
	return strings.Join(msgs, "; ")
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case loadedMsg:
		m.loading = false
		if msg.err != nil {
			m.status = statusMsg{msg.err.Error(), true}
			break
		}
		m.tickets = msg.tickets
		if f := lookupFailures(msg.tickets); f != "" {
			m.status = statusMsg{f, true}
		}
	case statusMsg:
		m.status = msg
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

// setFold folds or unfolds group g and moves the cursor to its title when folding.
func (m model) setFold(g string, fold bool) model {
	m.folded[g] = fold
	if fold {
		for i, r := range m.rows() {
			if r.Ticket == nil && r.Group == g {
				m.cursor = i
			}
		}
	}
	return m
}

func (m model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if m.filtering {
		switch k.Type {
		case tea.KeyEsc:
			m.filter, m.filtering = "", false
		case tea.KeyEnter:
			m.filtering = false
		case tea.KeyBackspace, tea.KeyDelete:
			if r := []rune(m.filter); len(r) > 0 {
				m.filter = string(r[:len(r)-1])
			}
		case tea.KeySpace:
			m.filter += " "
		case tea.KeyRunes:
			if !k.Alt {
				m.filter += string(k.Runes)
			}
		}
		m.cursor = 0
		return m, nil
	}
	if m.help {
		m.help = false
		return m, nil
	}
	rows := m.rows()
	sel := m.sel(len(rows))
	var row Row
	if len(rows) > 0 {
		row = rows[sel]
	}
	switch s := k.String(); s {
	case "q":
		return m, tea.Quit
	case "j", "down":
		m.cursor = min(sel+1, len(rows)-1)
	case "k", "up":
		m.cursor = max(sel-1, 0)
	case "g":
		m.cursor = 0
	case "G":
		m.cursor = len(rows) - 1
	case "/":
		m.filtering = true
	case "?":
		m.help = true
	case "esc":
		m.filter = ""
	case "r":
		if !m.loading {
			m.loading = true
			m.status = statusMsg{}
			return m, m.fetch()
		}
	case "enter", " ":
		if row.Group != "" && row.Ticket == nil {
			m = m.setFold(row.Group, !m.folded[row.Group])
		}
	case "h", "left":
		if row.Group != "" {
			m = m.setFold(row.Group, true)
		}
	case "l", "right":
		if row.Group != "" {
			m = m.setFold(row.Group, false)
		}
	default:
		if row.Ticket == nil {
			break
		}
		for _, a := range m.actions {
			if a.Key == s && a.Applies(row.Ticket, row.Branch) {
				return m, runAction(a, row)
			}
		}
	}
	return m, nil
}

func runAction(a Action, row Row) tea.Cmd {
	return func() tea.Msg {
		var last string
		err := a.Run(ActionContext{Ticket: row.Ticket, Branch: row.Branch, Notify: func(s string) { last = s }})
		if err != nil {
			return statusMsg{fmt.Sprintf("%s failed: %v", a.Label, err), true}
		}
		if last == "" {
			return nil
		}
		return statusMsg{last, false}
	}
}

func (m model) View() string {
	trunc := lipgloss.NewStyle().MaxWidth(m.width)
	if m.help {
		lines := []string{lipgloss.NewStyle().Bold(true).Render("Keys")}
		entries := navHelp
		for _, a := range m.actions {
			entries = append(entries, [2]string{a.Key, a.Label})
		}
		for _, e := range entries {
			lines = append(lines, cyan.Render(fmt.Sprintf("%-12s", e[0]))+e[1])
		}
		lines = append(lines, dim.Render("any key to close"))
		return box.Render(strings.Join(lines, "\n"))
	}

	rows := m.rows()
	sel := m.sel(len(rows))
	// Scroll window keeps the cursor in view; 3 lines reserved for filter, status and footer.
	height := max(1, m.height-3)
	start := max(0, min(sel-height/2, len(rows)-height))

	var lines []string
	switch {
	case m.loading && len(m.tickets) == 0:
		lines = append(lines, yellow.Render("Loading tickets…"))
	case len(rows) == 0:
		if m.filter != "" {
			lines = append(lines, dim.Render("no matches"))
		} else {
			lines = append(lines, dim.Render("no open tickets"))
		}
	default:
		for i := start; i < min(start+height, len(rows)); i++ {
			lines = append(lines, trunc.Render(rowLine(rows[i], i == sel)))
		}
	}

	filterLine := " "
	if m.filtering || m.filter != "" {
		cur := ""
		if m.filtering {
			cur = "▏"
		}
		filterLine = green.Render("/" + m.filter + cur)
	}
	statusStyle, statusText := green, m.status.text
	if m.status.err {
		statusStyle = red
	}
	if m.loading && len(m.tickets) > 0 {
		statusText = "refreshing…"
	}
	if statusText == "" {
		statusText = " "
	}
	var footer []string
	for _, a := range m.actions {
		footer = append(footer, a.Key+" "+a.Label)
	}
	footer = append(footer, "enter fold", "/ filter", "r refresh", "? help", "q quit")

	lines = append(lines, filterLine, trunc.Render(statusStyle.Render(statusText)), trunc.Render(dim.Render(strings.Join(footer, " · "))))
	return strings.Join(lines, "\n")
}

func prLabel(p PR) (string, lipgloss.Style) {
	if p.Status == "MERGED" {
		return "merged → " + p.Target, green
	}
	return "PR open → " + p.Target, cyan
}

// rowLine renders a row; the active row is plain inverse text since nested colors would reset the inverse.
func rowLine(r Row, active bool) string {
	if r.Ticket == nil {
		arrow := "▾"
		if r.Folded {
			arrow = "▸"
		}
		text := fmt.Sprintf("%s %s (%d)", arrow, r.Group, r.Count)
		if active {
			return inverse.Render(text)
		}
		return statusStyle(r.Group).Bold(true).Render(text)
	}

	t, b := r.Ticket, r.Branch
	if b != nil {
		var plain, styled []string
		merged := false
		for _, p := range b.PRs {
			label, style := prLabel(p)
			plain = append(plain, label)
			styled = append(styled, style.Render(label))
			merged = merged || p.Status == "MERGED"
		}
		if active {
			line := "      " + b.Repo + " " + b.Name
			if len(plain) > 0 {
				line += "  " + strings.Join(plain, ", ")
			}
			return inverse.Render(line)
		}
		name := b.Name
		if merged {
			name = dim.Render(name)
		}
		line := "      " + magenta.Render(b.Repo) + " " + name
		if len(styled) > 0 {
			line += "  " + strings.Join(styled, ", ")
		}
		return line
	}

	note := ""
	if len(t.Branches) == 0 {
		note = " — no branch"
		if t.BranchError != "" {
			note = " — branch lookup failed"
		}
	}
	if active {
		return inverse.Render("  " + t.Key + " [" + t.Status + "] " + t.Summary + note)
	}
	return "  " + keyStyle.Render(t.Key) + " " + statusStyle(t.Status).Render("["+t.Status+"]") + " " + t.Summary + dim.Render(note)
}
