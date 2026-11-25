package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
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

func createJira(c Config, client *http.Client) *Jira {
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(c.Email+":"+c.Token))
	return &Jira{baseURL: c.BaseURL, auth: auth, client: client}
}

func (j *Jira) get(path string, out any) error {
	req, err := http.NewRequest("GET", j.baseURL+path, nil)
	if err != nil {
		return &JiraError{Msg: err.Error()}
	}
	req.Header.Set("Authorization", j.auth)
	req.Header.Set("Accept", "application/json")
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
			out = append(out, Ticket{ID: i.ID, Key: i.Key, Summary: i.Fields.Summary, Status: i.Fields.Status.Name})
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
