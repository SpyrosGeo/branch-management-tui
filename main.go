package main

import (
	"fmt"
	"net/http"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	cfg, err := loadConfig(os.Getenv, configPath(os.Getenv))
	if err != nil {
		fmt.Fprintf(os.Stderr, "branch-tui: %v\n", err)
		os.Exit(1)
	}
	jira := createJira(cfg, http.DefaultClient)
	load := func() ([]Ticket, error) {
		return loadTickets(jira.SearchTickets, jira.Branches, jira.PullRequests, cfg.JQL)
	}
	actions := withKeymap([]Action{copyBranch(writeClipboard), openTicket(cfg.BaseURL, openBrowser)}, cfg.Keymap)
	if _, err := tea.NewProgram(newModel(load, actions), tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "branch-tui: %v\n", err)
		os.Exit(1)
	}
}
