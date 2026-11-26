package main

type Branch struct {
	Name string
	Repo string
	URL  string // empty when dev-status sends none
	PRs  []PR   // open or merged pull requests from this branch; declined ones are dropped
}

// PR is a pull request as seen from its source branch.
type PR struct {
	Status string // "OPEN" or "MERGED"
	Target string // destination branch
	URL    string // empty when dev-status sends none
}

type Ticket struct {
	ID       string
	Key      string
	Summary  string
	Status   string
	Branches []Branch
	// Set when a lookup for this ticket failed; the ticket still shows.
	BranchError string
	PRError     string
}

// Detail is a ticket's description and comments as plain text, fetched on demand.
type Detail struct {
	Key         string
	Summary     string
	Description string
	Comments    []Comment
}

type Comment struct {
	Author  string
	Created string // as Jira sends it, e.g. 2026-11-20T10:00:00.000+0000
	Body    string
}
