package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"github.com/atotto/clipboard"
)

type ActionContext struct {
	Ticket *Ticket
	Branch *Branch // nil on a ticket row
	Choice int     // index into the options Choose or Pick offered; -1 when there were none or text was typed
	Text   string  // what the user typed after choosing "Other…"
	Notify func(msg string)
	Show   func(d Detail) // opens the detail popup once the action finishes
	// SetStatus shows the ticket in the new status once the action finishes, without a reload.
	SetStatus func(status string)
	// Pick opens a picker once Run returns; then runs with the choice. other adds "Other…" for typed text.
	Pick func(title string, options []string, other bool, then func(ActionContext) error)
}

// Action: add a feature by appending one to the list in main.go; footer and help pick it up.
type Action struct {
	ID    string
	Key   string
	Label string
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
		ID:     "open-bitbucket",
		Key:    "b",
		Label:  "open in Bitbucket",
		Choose: chooseBranch,
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

func copyURL(baseURL string, copy func(string) error) Action {
	return copyText("copy-url", "Y", "copy ticket URL", func(t *Ticket) string { return baseURL + "/browse/" + t.Key }, copy)
}

func copyText(id, key, label string, text func(*Ticket) string, copy func(string) error) Action {
	return Action{
		ID:    id,
		Key:   key,
		Label: label,
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
		ID:    "copy-branch",
		Key:   "y",
		Label: "copy branch",
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
		ID:    "open-ticket",
		Key:   "o",
		Label: "open in browser",
		Run: func(c ActionContext) error {
			if err := open(baseURL + "/browse/" + c.Ticket.Key); err != nil {
				return err
			}
			c.Notify("opened " + c.Ticket.Key)
			return nil
		},
	}
}

func showDetail(fetch func(key string) (Detail, error)) Action {
	return Action{
		ID:    "show-detail",
		Key:   "s",
		Label: "show description and comments",
		Run: func(c ActionContext) error {
			d, err := fetch(c.Ticket.Key)
			if err != nil {
				return err
			}
			c.Show(d)
			return nil
		},
	}
}

// reservedKeys are the navigation keys handled before actions in model.key; an action bound to one never runs.
var reservedKeys = map[string]bool{
	"q": true, "j": true, "k": true, "g": true, "G": true, "/": true, "?": true, "r": true,
	"h": true, "l": true, " ": true, "enter": true, "esc": true, "up": true, "down": true,
	"left": true, "right": true, "ctrl+c": true,
}

// withKeymap applies the configured bindings and rejects unknown action IDs, reserved keys and duplicates.
func withKeymap(actions []Action, keymap map[string]string) ([]Action, error) {
	ids := map[string]bool{}
	for _, a := range actions {
		ids[a.ID] = true
	}
	var known []string
	for id := range ids {
		known = append(known, id)
	}
	sort.Strings(known)
	var problems []string
	for id := range keymap {
		if !ids[id] {
			problems = append(problems, fmt.Sprintf("unknown action %q (known: %s)", id, strings.Join(known, ", ")))
		}
	}
	out := make([]Action, len(actions))
	byKey := map[string]string{}
	for i, a := range actions {
		if k := keymap[a.ID]; k != "" {
			a.Key = k
		}
		if reservedKeys[a.Key] {
			problems = append(problems, fmt.Sprintf("%s: %q is a navigation key", a.ID, a.Key))
		}
		if other, ok := byKey[a.Key]; ok {
			problems = append(problems, fmt.Sprintf("%s and %s are both bound to %q", other, a.ID, a.Key))
		}
		byKey[a.Key] = a.ID
		out[i] = a
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, errors.New("keymap: " + strings.Join(problems, "; "))
	}
	return out, nil
}

// transitionLabels names each transition by its target status, adding the transition name where two share one.
func transitionLabels(ts []Transition) []string {
	seen := map[string]int{}
	for _, t := range ts {
		seen[t.To]++
	}
	var out []string
	for _, t := range ts {
		if seen[t.To] > 1 {
			out = append(out, t.Name+" → "+t.To)
		} else {
			out = append(out, t.To)
		}
	}
	return out
}

func transitionTicket(baseURL string, list func(key string) ([]Transition, error), apply func(key, id string) error, open func(string) error) Action {
	return Action{
		ID:    "transition",
		Key:   "t",
		Label: "transition ticket",
		Run: func(c ActionContext) error {
			key := c.Ticket.Key
			ts, err := list(key)
			if err != nil {
				return err
			}
			if len(ts) == 0 {
				return errors.New(key + " has no transitions")
			}
			c.Pick("Transition "+key+" ["+c.Ticket.Status+"]", transitionLabels(ts), false, func(c ActionContext) error {
				t := ts[c.Choice]
				if t.NeedsFields {
					if err := open(baseURL + "/browse/" + key); err != nil {
						return err
					}
					c.Notify(fmt.Sprintf("%s: %q needs fields, opened in browser", key, t.Name))
					return nil
				}
				if err := apply(key, t.ID); err != nil {
					return err
				}
				c.SetStatus(t.To)
				c.Notify(key + " → " + t.To)
				return nil
			})
			return nil
		},
	}
}

func assignTicket(users func(key string) ([]User, error), me func() (string, error), assign func(key, accountID string) error) Action {
	return Action{
		ID:    "assign",
		Key:   "a",
		Label: "assign ticket",
		Run: func(c ActionContext) error {
			key := c.Ticket.Key
			us, err := users(key)
			if err != nil {
				return err
			}
			opts := []string{"Assign to me", "Unassign"}
			for _, u := range us {
				opts = append(opts, u.Name)
			}
			c.Pick("Assign "+key, opts, false, func(c ActionContext) error {
				id, name := "", ""
				switch c.Choice {
				case 0:
					var err error
					if id, err = me(); err != nil {
						return err
					}
					name = "me"
				case 1:
				default:
					id, name = us[c.Choice-2].AccountID, us[c.Choice-2].Name
				}
				if err := assign(key, id); err != nil {
					return err
				}
				if id == "" {
					c.Notify(key + " unassigned")
				} else {
					c.Notify(key + " → " + name)
				}
				return nil
			})
			return nil
		},
	}
}

// prURL is the branch's open PR into target, else Bitbucket's create-PR page from the branch into target.
func prURL(b *Branch, target string) (string, error) {
	for _, p := range b.PRs {
		if p.Status == "OPEN" && p.Target == target && isWebURL(p.URL) {
			return p.URL, nil
		}
	}
	if b.Repo == "?" {
		return "", errors.New("no repo for " + b.Name)
	}
	// shortcut: assumes Bitbucket Cloud, like bitbucketURL.
	return "https://bitbucket.org/" + b.Repo + "/pull-requests/new?" + url.Values{"source": {b.Name}, "dest": {target}}.Encode(), nil
}

func openPR(targets []string, open func(string) error) Action {
	return Action{
		ID:     "open-pr",
		Key:    "p",
		Label:  "open PR to a target branch",
		Choose: chooseBranch,
		Run: func(c ActionContext) error {
			b := rowBranches(c.Ticket, c.Branch)[c.Choice]
			c.Pick("Target branch for "+b.Name, targets, true, func(c ActionContext) error {
				target := c.Text
				if target == "" {
					target = targets[c.Choice]
				}
				u, err := prURL(b, target)
				if err != nil {
					return err
				}
				if err := open(u); err != nil {
					return err
				}
				c.Notify("opened " + u)
				return nil
			})
			return nil
		},
	}
}
