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
	Type     string // issue type, e.g. "Bug" or "Story"
	Branches []Branch
	// Set when a lookup for this ticket failed; the ticket still shows.
	BranchError string
	PRError     string
}
