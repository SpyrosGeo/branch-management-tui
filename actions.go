package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/atotto/clipboard"
)

type ActionContext struct {
	Ticket *Ticket
	Branch *Branch // nil on a ticket row
	Choice int     // index into the options Choose returned; -1 when the action has no Choose
	Notify func(msg string)
}

// Action: add a feature by appending one to the list in main.go; footer and help pick it up.
type Action struct {
	ID      string
	Key     string
	Label   string
	Applies func(t *Ticket, b *Branch) bool
	// Choose, when set, runs first and lists options; one option runs directly, more open the picker.
	Choose func(t *Ticket, b *Branch) (title string, options []string, err error)
	Run    func(ctx ActionContext) error
}

// rowBranches is the branch on a branch row, or every branch of the ticket on a ticket row.
func rowBranches(t *Ticket, b *Branch) []*Branch {
	if b != nil {
		return []*Branch{b}
	}
	var out []*Branch
	for i := range t.Branches {
		out = append(out, &t.Branches[i])
	}
	return out
}

func chooseBranch(t *Ticket, b *Branch) (string, []string, error) {
	var opts []string
	for _, x := range rowBranches(t, b) {
		opts = append(opts, x.Repo+" "+x.Name)
	}
	if len(opts) == 0 {
		return "", nil, errors.New(t.Key + " has no branch")
	}
	return "Branch for " + t.Key, opts, nil
}

// isWebURL keeps OS openers away from file paths or other schemes that might come back from dev-status.
func isWebURL(u string) bool {
	return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")
}

// bitbucketURL prefers the open PR, then a merged PR, then the branch page.
func bitbucketURL(b *Branch) (string, error) {
	for _, status := range []string{"OPEN", "MERGED"} {
		for _, p := range b.PRs {
			if p.Status == status && isWebURL(p.URL) {
				return p.URL, nil
			}
		}
	}
	if isWebURL(b.URL) {
		return b.URL, nil
	}
	if b.Repo == "?" {
		return "", errors.New("no URL or repo for " + b.Name)
	}
	// shortcut: assumes Bitbucket Cloud; read the host from config if Server/Data Center repos show up.
	parts := strings.Split(b.Name, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return "https://bitbucket.org/" + b.Repo + "/branch/" + strings.Join(parts, "/"), nil
}

func openBitbucket(open func(string) error) Action {
	return Action{
		ID:      "open-bitbucket",
		Key:     "b",
		Label:   "open in Bitbucket",
		Applies: func(*Ticket, *Branch) bool { return true },
		Choose:  chooseBranch,
		Run: func(c ActionContext) error {
			b := rowBranches(c.Ticket, c.Branch)[c.Choice]
			u, err := bitbucketURL(b)
			if err != nil {
				return err
			}
			if err := open(u); err != nil {
				return err
			}
			c.Notify("opened " + u)
			return nil
		},
	}
}

func copyKey(copy func(string) error) Action {
	return copyText("copy-key", "Y", "copy key", func(t *Ticket) string { return t.Key }, copy)
}

func copySummary(copy func(string) error) Action {
	return copyText("copy-summary", "S", "copy summary", func(t *Ticket) string { return t.Summary }, copy)
}

func copyURL(baseURL string, copy func(string) error) Action {
	return copyText("copy-url", "U", "copy URL", func(t *Ticket) string { return baseURL + "/browse/" + t.Key }, copy)
}

func copyText(id, key, label string, text func(*Ticket) string, copy func(string) error) Action {
	return Action{
		ID:      id,
		Key:     key,
		Label:   label,
		Applies: func(*Ticket, *Branch) bool { return true },
		Run: func(c ActionContext) error {
			s := text(c.Ticket)
			if err := copy(s); err != nil {
				return err
			}
			c.Notify("copied " + s)
			return nil
		},
	}
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

// openBrowser hands url to the OS opener; on WSL the Windows browser is used.
func openBrowser(url string) error {
	cmds := [][]string{{"xdg-open", url}, {"wslview", url}, {"explorer.exe", url}}
	switch {
	case runtime.GOOS == "darwin":
		cmds = [][]string{{"open", url}}
	case os.Getenv("WSL_DISTRO_NAME") != "":
		cmds = [][]string{{"wslview", url}, {"explorer.exe", url}}
	}
	for _, c := range cmds {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		cmd := exec.Command(c[0], c[1:]...) // nil Stdout/Stderr: opener output must not draw over the TUI
		if err := cmd.Start(); err != nil {
			return err
		}
		go cmd.Wait() // reap; explorer.exe exits 1 even on success, so the result is ignored
		return nil
	}
	return errors.New("no browser opener found (xdg-open, wslview or explorer.exe)")
}

func openTicket(baseURL string, open func(string) error) Action {
	return Action{
		ID:      "open-ticket",
		Key:     "o",
		Label:   "open in browser",
		Applies: func(*Ticket, *Branch) bool { return true },
		Run: func(c ActionContext) error {
			if err := open(baseURL + "/browse/" + c.Ticket.Key); err != nil {
				return err
			}
			c.Notify("opened " + c.Ticket.Key)
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
