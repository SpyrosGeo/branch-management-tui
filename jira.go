package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"unicode"
)

type JiraError struct {
	Msg    string
	Status int
}

func (e *JiraError) Error() string { return e.Msg }

// BranchSource is a swappable source of branches for an issue; dev-status is undocumented and may change.
type BranchSource func(issueID string) ([]Branch, error)

// PullRequest is one pull request as returned by dev-status, before it is attached to a Branch.
type PullRequest struct {
	Repo   string
	Branch string // source branch
	Status string // OPEN, MERGED or DECLINED
	Target string // destination branch
	URL    string
}

// PRSource is a swappable source of pull requests for an issue.
type PRSource func(issueID string) ([]PullRequest, error)

type Jira struct {
	baseURL string
	auth    string
	client  *http.Client
	mu      sync.Mutex
	me      string // current user's accountId, fetched on first use
}

type searchResponse struct {
	Issues []struct {
		ID     string `json:"id"`
		Key    string `json:"key"`
		Fields struct {
			Summary string `json:"summary"`
			Status  struct {
				Name string `json:"name"`
			} `json:"status"`
		} `json:"fields"`
	} `json:"issues"`
	NextPageToken string `json:"nextPageToken"`
	IsLast        bool   `json:"isLast"`
}

type devStatusResponse struct {
	Detail []struct {
		Branches []struct {
			Name       string `json:"name"`
			URL        string `json:"url"`
			Repository struct {
				Name string `json:"name"`
			} `json:"repository"`
		} `json:"branches"`
	} `json:"detail"`
}

type devStatusPRResponse struct {
	Detail []struct {
		PullRequests []struct {
			Status         string `json:"status"`
			RepositoryName string `json:"repositoryName"`
			URL            string `json:"url"`
			Source         struct {
				Branch string `json:"branch"`
			} `json:"source"`
			Destination struct {
				Branch string `json:"branch"`
			} `json:"destination"`
		} `json:"pullRequests"`
	} `json:"detail"`
}

// adf is an Atlassian Document Format node, the rich text Jira API v3 returns.
type adf struct {
	Type    string         `json:"type"`
	Text    string         `json:"text"`
	Attrs   map[string]any `json:"attrs"`
	Content []adf          `json:"content"`
}

type issueResponse struct {
	Fields struct {
		Summary     string `json:"summary"`
		Description adf    `json:"description"`
		Comment     struct {
			Comments []struct {
				Author struct {
					DisplayName string `json:"displayName"`
				} `json:"author"`
				Created string `json:"created"`
				Body    adf    `json:"body"`
			} `json:"comments"`
		} `json:"comment"`
	} `json:"fields"`
}

func createJira(c Config, client *http.Client) *Jira {
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(c.Email+":"+c.Token))
	return &Jira{baseURL: c.BaseURL, auth: auth, client: client}
}

func (j *Jira) get(path string, out any) error { return j.do("GET", path, nil, out) }

// do sends body as JSON when set and decodes the response into out when set.
func (j *Jira) do(method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return &JiraError{Msg: err.Error()}
		}
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, j.baseURL+path, r)
	if err != nil {
		return &JiraError{Msg: err.Error()}
	}
	req.Header.Set("Authorization", j.auth)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := j.client.Do(req)
	if err != nil {
		return &JiraError{Msg: fmt.Sprintf("Network error reaching %s: %v", j.baseURL, err)}
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		return &JiraError{Msg: "Jira rejected credentials (401): check JIRA_EMAIL and JIRA_API_TOKEN", Status: 401}
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return &JiraError{Msg: fmt.Sprintf("Jira %d on %s", res.StatusCode, strings.SplitN(path, "?", 2)[0]), Status: res.StatusCode}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// SearchTickets returns tickets without branches, following nextPageToken.
func (j *Jira) SearchTickets(jql string) ([]Ticket, error) {
	var out []Ticket
	pageToken := ""
	for {
		q := url.Values{"jql": {jql}, "fields": {"summary,status"}, "maxResults": {"100"}}
		if pageToken != "" {
			q.Set("nextPageToken", pageToken)
		}
		var data searchResponse
		if err := j.get("/rest/api/3/search/jql?"+q.Encode(), &data); err != nil {
			return nil, err
		}
		for _, i := range data.Issues {
			out = append(out, Ticket{ID: i.ID, Key: i.Key, Summary: stripControl(i.Fields.Summary), Status: i.Fields.Status.Name})
		}
		if data.IsLast || data.NextPageToken == "" {
			return out, nil
		}
		pageToken = data.NextPageToken
	}
}

func (j *Jira) Branches(issueID string) ([]Branch, error) {
	q := url.Values{"issueId": {issueID}, "applicationType": {"bitbucket"}, "dataType": {"branch"}}
	var data devStatusResponse
	if err := j.get("/rest/dev-status/latest/issue/detail?"+q.Encode(), &data); err != nil {
		return nil, err
	}
	var out []Branch
	for _, d := range data.Detail {
		for _, b := range d.Branches {
			repo := b.Repository.Name
			if repo == "" {
				repo = "?"
			}
			out = append(out, Branch{Name: b.Name, Repo: repo, URL: b.URL})
		}
	}
	return out, nil
}

func (j *Jira) PullRequests(issueID string) ([]PullRequest, error) {
	q := url.Values{"issueId": {issueID}, "applicationType": {"bitbucket"}, "dataType": {"pullrequest"}}
	var data devStatusPRResponse
	if err := j.get("/rest/dev-status/latest/issue/detail?"+q.Encode(), &data); err != nil {
		return nil, err
	}
	var out []PullRequest
	for _, d := range data.Detail {
		for _, p := range d.PullRequests {
			out = append(out, PullRequest{Repo: p.RepositoryName, Branch: p.Source.Branch, Status: p.Status, Target: p.Destination.Branch, URL: p.URL})
		}
	}
	return out, nil
}

// IssueDetail fetches a ticket's description and comments.
// shortcut: uses the comments embedded in the issue (Jira caps that list); page /comment if long threads get cut.
func (j *Jira) IssueDetail(key string) (Detail, error) {
	var data issueResponse
	if err := j.get(issuePath(key)+"?fields=summary,description,comment", &data); err != nil {
		return Detail{}, err
	}
	d := Detail{Key: key, Summary: stripControl(data.Fields.Summary), Description: adfText(data.Fields.Description)}
	for _, c := range data.Fields.Comment.Comments {
		d.Comments = append(d.Comments, Comment{Author: stripControl(c.Author.DisplayName), Created: c.Created, Body: adfText(c.Body)})
	}
	return d, nil
}

func issuePath(key string) string { return "/rest/api/3/issue/" + url.PathEscape(key) }

// Transitions lists the transitions Jira offers for a ticket in its current status.
func (j *Jira) Transitions(key string) ([]Transition, error) {
	var data struct {
		Transitions []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			To   struct {
				Name string `json:"name"`
			} `json:"to"`
			Fields map[string]struct {
				Required        bool `json:"required"`
				HasDefaultValue bool `json:"hasDefaultValue"`
			} `json:"fields"`
		} `json:"transitions"`
	}
	if err := j.get(issuePath(key)+"/transitions?expand=transitions.fields", &data); err != nil {
		return nil, err
	}
	var out []Transition
	for _, t := range data.Transitions {
		needs := false
		for _, f := range t.Fields {
			needs = needs || (f.Required && !f.HasDefaultValue)
		}
		out = append(out, Transition{ID: t.ID, Name: stripControl(t.Name), To: stripControl(t.To.Name), NeedsFields: needs})
	}
	return out, nil
}

func (j *Jira) Transition(key, id string) error {
	return j.do("POST", issuePath(key)+"/transitions", map[string]any{"transition": map[string]string{"id": id}}, nil)
}

// AssignableUsers lists the people Jira allows as assignee of a ticket, sorted by name.
// shortcut: one page of up to 1000; page the endpoint if a project has more assignable users.
func (j *Jira) AssignableUsers(key string) ([]User, error) {
	var data []struct {
		AccountID   string `json:"accountId"`
		DisplayName string `json:"displayName"`
		AccountType string `json:"accountType"`
		Active      bool   `json:"active"`
	}
	q := url.Values{"issueKey": {key}, "maxResults": {"1000"}}
	if err := j.get("/rest/api/3/user/assignable/search?"+q.Encode(), &data); err != nil {
		return nil, err
	}
	var out []User
	for _, u := range data {
		if u.Active && u.AccountType == "atlassian" { // skip deactivated users and app/bot accounts
			out = append(out, User{AccountID: u.AccountID, Name: stripControl(u.DisplayName)})
		}
	}
	sort.Slice(out, func(a, b int) bool { return strings.ToLower(out[a].Name) < strings.ToLower(out[b].Name) })
	return out, nil
}

// Assign sets a ticket's assignee; an empty accountID unassigns it.
func (j *Jira) Assign(key, accountID string) error {
	var id any // null unassigns
	if accountID != "" {
		id = accountID
	}
	return j.do("PUT", issuePath(key)+"/assignee", map[string]any{"accountId": id}, nil)
}

// Myself returns the current user's accountId, asking Jira only once.
func (j *Jira) Myself() (string, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.me != "" {
		return j.me, nil
	}
	var data struct {
		AccountID string `json:"accountId"`
	}
	if err := j.get("/rest/api/3/myself", &data); err != nil {
		return "", err
	}
	j.me = data.AccountID
	return j.me, nil
}

// stripControl drops control characters except newline and tab, so Jira users cannot send escape sequences to the terminal.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
}

// adfText flattens ADF to plain text: one line per block, a blank line between top-level blocks.
func adfText(n adf) string {
	var b strings.Builder
	writeADF(&b, n)
	return strings.TrimSpace(b.String())
}

func writeADF(b *strings.Builder, n adf) {
	endLine := func() {
		if s := b.String(); s != "" && !strings.HasSuffix(s, "\n") {
			b.WriteString("\n")
		}
	}
	switch n.Type {
	case "text":
		b.WriteString(stripControl(n.Text))
		return
	case "hardBreak":
		b.WriteString("\n")
		return
	case "mention", "emoji", "inlineCard", "status":
		for _, k := range []string{"text", "url", "shortName"} {
			if s, ok := n.Attrs[k].(string); ok && s != "" {
				b.WriteString(stripControl(s))
				return
			}
		}
		return
	case "rule":
		endLine()
		b.WriteString("────\n")
		return
	case "listItem":
		b.WriteString("• ")
	case "orderedList":
		// shortcut: numbers from 1 and ignores attrs.order; nested lists are not indented.
		for i, item := range n.Content {
			endLine()
			fmt.Fprintf(b, "%d. ", i+1)
			for _, c := range item.Content {
				writeADF(b, c)
			}
		}
		endLine()
		return
	case "doc":
		for _, c := range n.Content {
			writeADF(b, c)
			endLine()
			b.WriteString("\n")
		}
		return
	}
	for _, c := range n.Content {
		writeADF(b, c)
	}
	if n.Type != "" {
		endLine()
	}
}

// attachPRs adds open and merged pull requests to their source branch; declined ones are dropped.
func attachPRs(branches []Branch, prs []PullRequest) {
	for i := range branches {
		b := &branches[i]
		for _, p := range prs {
			// shortcut: matches on branch name, plus repo when dev-status sends one; upgrade if repos share branch names without repositoryName.
			if p.Branch != b.Name || (p.Repo != "" && p.Repo != b.Repo) || (p.Status != "OPEN" && p.Status != "MERGED") {
				continue
			}
			b.PRs = append(b.PRs, PR{Status: p.Status, Target: p.Target, URL: p.URL})
		}
	}
}

// loadTickets fetches branches and pull requests with at most 5 tickets in flight, preserving order.
// A failed lookup keeps the ticket and records BranchError or PRError.
func loadTickets(search func(jql string) ([]Ticket, error), branches BranchSource, prs PRSource, jql string) ([]Ticket, error) {
	found, err := search(jql)
	if err != nil {
		return nil, err
	}
	sem := make(chan struct{}, 5)
	var wg sync.WaitGroup
	for i := range found {
		t := &found[i]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			b, err := branches(t.ID)
			if err != nil {
				t.BranchError = err.Error()
				return
			}
			t.Branches = b
			if len(b) == 0 {
				return
			}
			p, err := prs(t.ID)
			if err != nil {
				t.PRError = err.Error()
				return
			}
			attachPRs(t.Branches, p)
		}()
	}
	wg.Wait()
	return found, nil
}
