package main

import (
	"sort"
	"strings"
)

// Row is a group title when Ticket is nil, a ticket row when Branch is nil, else a branch row.
// Group is the status name on every row so actions like folding can find the row's group.
type Row struct {
	Group  string
	Count  int  // group title only: tickets shown in the group
	Folded bool // group title only
	Ticket *Ticket
	Branch *Branch
}

// statusRank puts In Progress, then Issue Found, then everything else alphabetically.
func statusRank(s string) int {
	switch s {
	case "In Progress":
		return 0
	case "Issue Found":
		return 1
	}
	return 2
}

// buildRows groups tickets by status. A group with a filter match is unfolded while the filter is set.
func buildRows(tickets []Ticket, filter string, folded map[string]bool) []Row {
	f := strings.ToLower(filter)
	hit := func(s ...string) bool {
		for _, x := range s {
			if strings.Contains(strings.ToLower(x), f) {
				return true
			}
		}
		return false
	}

	groups := map[string][]Row{}
	var order []string
	for i := range tickets {
		t := &tickets[i]
		ticketHit := hit(t.Key, t.Summary)
		var branches []Row
		for j := range t.Branches {
			if b := &t.Branches[j]; ticketHit || hit(b.Repo, b.Name) {
				branches = append(branches, Row{Group: t.Status, Ticket: t, Branch: b})
			}
		}
		if !ticketHit && len(branches) == 0 {
			continue
		}
		if _, ok := groups[t.Status]; !ok {
			order = append(order, t.Status)
		}
		groups[t.Status] = append(append(groups[t.Status], Row{Group: t.Status, Ticket: t}), branches...)
	}
	sort.SliceStable(order, func(a, b int) bool {
		ra, rb := statusRank(order[a]), statusRank(order[b])
		if ra != rb {
			return ra < rb
		}
		return order[a] < order[b]
	})

	var rows []Row
	for _, g := range order {
		count := 0
		for _, r := range groups[g] {
			if r.Branch == nil {
				count++
			}
		}
		isFolded := folded[g] && filter == ""
		rows = append(rows, Row{Group: g, Count: count, Folded: isFolded})
		if !isFolded {
			rows = append(rows, groups[g]...)
		}
	}
	return rows
}
