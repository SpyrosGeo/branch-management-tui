package main

import (
	"encoding/base64"
	"fmt"
	"os"

	"github.com/atotto/clipboard"
)

type ActionContext struct {
	Ticket *Ticket
	Branch *Branch // nil on a ticket row
	Notify func(msg string)
}

// Action: add a feature by appending one to the list in main.go; footer and help pick it up.
type Action struct {
	ID      string
	Key     string
	Label   string
	Applies func(t *Ticket, b *Branch) bool
	Run     func(ctx ActionContext) error
}

func writeClipboard(text string) error {
	if err := clipboard.WriteAll(text); err != nil {
		// OSC 52: terminal-side clipboard, works over SSH and when no clipboard binary exists.
		fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\a", base64.StdEncoding.EncodeToString([]byte(text)))
	}
	return nil
}

func copyBranch(copy func(string) error) Action {
	return Action{
		ID:      "copy-branch",
		Key:     "y",
		Label:   "copy branch",
		Applies: func(*Ticket, *Branch) bool { return true },
		Run: func(c ActionContext) error {
			b := c.Branch
			if b == nil && len(c.Ticket.Branches) > 0 {
				b = &c.Ticket.Branches[0]
			}
			if b == nil {
				c.Notify(c.Ticket.Key + ": nothing to copy")
				return nil
			}
			if err := copy(b.Name); err != nil {
				return err
			}
			c.Notify("copied " + b.Name)
			return nil
		},
	}
}

func withKeymap(actions []Action, keymap map[string]string) []Action {
	out := make([]Action, len(actions))
	for i, a := range actions {
		if k := keymap[a.ID]; k != "" {
			a.Key = k
		}
		out[i] = a
	}
	return out
}
