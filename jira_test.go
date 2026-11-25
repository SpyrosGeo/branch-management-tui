package main

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func serveFixture(t *testing.T, h func(w http.ResponseWriter, r *http.Request) string) *Jira {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile("testdata/" + h(w, r))
		if err != nil {
			t.Error(err)
		}
		w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return createJira(Config{BaseURL: srv.URL, Email: "me@x", Token: "t"}, srv.Client())
}

func TestSearchTicketsPaginatesWithAuth(t *testing.T) {
	var auth string
	j := serveFixture(t, func(w http.ResponseWriter, r *http.Request) string {
		auth = r.Header.Get("Authorization")
		if r.URL.Query().Get("nextPageToken") == "page2" {
			return "search-page2.json"
		}
		return "search-page1.json"
	})
	tickets, err := j.SearchTickets("status = x")
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, tk := range tickets {
		keys = append(keys, tk.Key)
	}
	if !reflect.DeepEqual(keys, []string{"P00535-43", "P00535-41", "P00268-1431"}) {
		t.Errorf("keys %v", keys)
	}
	if tickets[1].Status != "Ready For QA" || tickets[1].Type != "Bug" {
		t.Errorf("status %q, type %q", tickets[1].Status, tickets[1].Type)
	}
	if want := "Basic " + base64.StdEncoding.EncodeToString([]byte("me@x:t")); auth != want {
		t.Errorf("auth %q", auth)
	}
}

func TestBranchesFromDevStatus(t *testing.T) {
	j := serveFixture(t, func(w http.ResponseWriter, r *http.Request) string {
		if r.URL.Path != "/rest/dev-status/latest/issue/detail" || r.URL.Query().Get("applicationType") != "bitbucket" {
			t.Errorf("unexpected request %s", r.URL)
		}
		return "dev-status-10041.json"
	})
	got, err := j.Branches("10041")
	if err != nil {
		t.Fatal(err)
	}
	want := []Branch{
		{Name: "bugfix/P00535-41-list-view-–-screen-size-issue", Repo: "pmmretail/mediamarkt", URL: "https://bitbucket.org/x"},
		{Name: "P00535-41-followup", Repo: "pmmretail/ecomm-ui-bo"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestUnauthorizedNamesEnvVars(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	defer srv.Close()
	_, err := createJira(Config{BaseURL: srv.URL}, srv.Client()).SearchTickets("x")
	if err == nil || !strings.Contains(err.Error(), "JIRA_EMAIL and JIRA_API_TOKEN") {
		t.Errorf("err %v", err)
	}
}

func TestLoadTicketsKeepsFailuresAndCapsConcurrency(t *testing.T) {
	var found []Ticket
	for i := range 12 {
		found = append(found, Ticket{ID: string(rune('a' + i)), Key: "K-" + string(rune('a'+i))})
	}
	var mu sync.Mutex
	inFlight, peak := 0, 0
	branches := func(id string) ([]Branch, error) {
		mu.Lock()
		inFlight++
		peak = max(peak, inFlight)
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		if id == "d" {
			return nil, errors.New("boom")
		}
		return []Branch{{Name: "b-" + id, Repo: "r"}}, nil
	}
	prs := func(id string) ([]PullRequest, error) {
		if id == "e" {
			return nil, errors.New("pr boom")
		}
		return []PullRequest{{Repo: "r", Branch: "b-" + id, Status: "MERGED", Target: "develop"}}, nil
	}
	tickets, err := loadTickets(func(string) ([]Ticket, error) { return found, nil }, branches, prs, "jql")
	if err != nil {
		t.Fatal(err)
	}
	if peak != 5 {
		t.Errorf("peak %d", peak)
	}
	for i, tk := range tickets {
		if tk.Key != found[i].Key {
			t.Errorf("order changed at %d: %s", i, tk.Key)
		}
	}
	if tickets[3].BranchError != "boom" || len(tickets[3].Branches) != 0 {
		t.Errorf("failed ticket %+v", tickets[3])
	}
	if !reflect.DeepEqual(tickets[4].Branches, []Branch{{Name: "b-e", Repo: "r"}}) || tickets[4].PRError != "pr boom" {
		t.Errorf("PR failure should keep branches: %+v", tickets[4])
	}
	if !reflect.DeepEqual(tickets[5].Branches[0].PRs, []PR{{Status: "MERGED", Target: "develop"}}) {
		t.Errorf("ticket 5 %+v", tickets[5])
	}
}

func TestPullRequestsAttachToBranches(t *testing.T) {
	j := serveFixture(t, func(w http.ResponseWriter, r *http.Request) string {
		if r.URL.Query().Get("dataType") == "pullrequest" {
			return "dev-status-pr-10041.json"
		}
		return "dev-status-10041.json"
	})
	branches, err := j.Branches("10041")
	if err != nil {
		t.Fatal(err)
	}
	prs, err := j.PullRequests("10041")
	if err != nil {
		t.Fatal(err)
	}
	attachPRs(branches, prs)
	if got := branches[0].PRs; !reflect.DeepEqual(got, []PR{{Status: "MERGED", Target: "develop", URL: "https://bitbucket.org/pmmretail/mediamarkt/pull-requests/120"}}) {
		t.Errorf("merged branch %v", got)
	}
	// The declined PR into develop is dropped; the open one into main stays.
	if got := branches[1].PRs; !reflect.DeepEqual(got, []PR{{Status: "OPEN", Target: "main"}}) {
		t.Errorf("followup branch %v", got)
	}
}

func TestAttachPRsMatchesRepo(t *testing.T) {
	b := []Branch{{Name: "develop-x", Repo: "a"}}
	attachPRs(b, []PullRequest{{Repo: "b", Branch: "develop-x", Status: "MERGED", Target: "main"}})
	if len(b[0].PRs) != 0 {
		t.Errorf("PR from another repo attached: %v", b[0].PRs)
	}
}
