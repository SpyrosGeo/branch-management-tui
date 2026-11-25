package main

import "strings"

// Row is a ticket row when Branch is nil; a branch row carries its parent ticket.
type Row struct {
	Ticket *Ticket
	Branch *Branch
}

func buildRows(tickets []Ticket, filter string) []Row {
	f := strings.ToLower(filter)
	hit := func(s ...string) bool {
		for _, x := range s {
			if strings.Contains(strings.ToLower(x), f) {
				return true
			}
		}
		return false
	}
	var rows []Row
	for i := range tickets {
		t := &tickets[i]
		ticketHit := hit(t.Key, t.Summary)
		var branches []*Branch
		for j := range t.Branches {
			if b := &t.Branches[j]; ticketHit || hit(b.Repo, b.Name) {
				branches = append(branches, b)
			}
		}
		if !ticketHit && len(branches) == 0 {
			continue
		}
		rows = append(rows, Row{Ticket: t})
		for _, b := range branches {
			rows = append(rows, Row{Ticket: t, Branch: b})
		}
	}
	return rows
}
