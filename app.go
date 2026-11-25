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
	dim      = lipgloss.NewStyle().Faint(true)
	inverse  = lipgloss.NewStyle().Reverse(true)
	box      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)

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
	return model{load: load, actions: actions, loading: true, height: 24}
}

func (m model) Init() tea.Cmd { return m.fetch() }

func (m model) fetch() tea.Cmd {
	return func() tea.Msg {
		t, err := m.load()
		return loadedMsg{t, err}
	}
}

func (m model) rows() []Row { return buildRows(m.tickets, m.filter) }

func (m model) sel(n int) int { return max(0, min(m.cursor, n-1)) }

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
		var failed []string
		first := ""
		for _, t := range msg.tickets {
			if t.BranchError != "" {
				failed = append(failed, t.Key)
				if first == "" {
					first = t.BranchError
				}
			}
		}
		if len(failed) > 0 {
			m.status = statusMsg{fmt.Sprintf("branch lookup failed for %s: %s", strings.Join(failed, ", "), first), true}
		}
	case statusMsg:
		m.status = msg
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
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
	default:
		if len(rows) == 0 {
			break
		}
		row := rows[sel]
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
			lines = append(lines, cyan.Render(fmt.Sprintf("%-10s", e[0]))+e[1])
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
			lines = append(lines, dim.Render("no in-progress tickets"))
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
	footer = append(footer, "/ filter", "r refresh", "? help", "q quit")

	lines = append(lines, filterLine, trunc.Render(statusStyle.Render(statusText)), trunc.Render(dim.Render(strings.Join(footer, " · "))))
	return strings.Join(lines, "\n")
}

// rowLine renders a row; the active row is plain inverse text since nested colors would reset the inverse.
func rowLine(r Row, active bool) string {
	t, b := r.Ticket, r.Branch
	if b != nil {
		if active {
			return inverse.Render("    " + b.Repo + " " + b.Name)
		}
		return "    " + magenta.Render(b.Repo) + " " + b.Name
	}
	note := ""
	if len(t.Branches) == 0 {
		note = " — no branch"
		if t.BranchError != "" {
			note = " — branch lookup failed"
		}
	}
	if active {
		return inverse.Render(t.Key + " [" + t.Status + "] " + t.Summary + note)
	}
	return keyStyle.Render(t.Key) + " " + yellow.Render("["+t.Status+"]") + " " + t.Summary + dim.Render(note)
}
