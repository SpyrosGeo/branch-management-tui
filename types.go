package main

type Branch struct {
	Name string
	Repo string
}

type Ticket struct {
	ID       string
	Key      string
	Summary  string
	Status   string
	Branches []Branch
	// Set when the branch lookup for this ticket failed; the ticket still shows.
	BranchError string
}
