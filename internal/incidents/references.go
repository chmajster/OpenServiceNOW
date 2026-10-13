package incidents

import "github.com/chmajster/OpenServiceNOW/internal/workrecords"

func ReferenceScope(a Actor) (workrecords.ReferenceScope, error) {
	if !a.Has("read") && !a.Has("read_own") {
		return workrecords.ReferenceScope{}, workrecords.ErrForbidden
	}
	where, args := listWhere(a, Filter{View: "all"})
	return workrecords.ReferenceScope{Kind: "incident", Root: "incidents", ID: "i.id", Number: "i.number", Title: "i.title", State: "i.state", Version: "i.version", CreatedAt: "i.created_at", UpdatedAt: "i.updated_at", GroupID: "w.assignment_group_id", GroupName: "COALESCE(g.name,'')", AssignedTo: "w.assigned_to", ReporterID: "i.reporter_id", Active: "i.state NOT IN('RESOLVED','CLOSED','CANCELLED')", From: recordJoins, Where: where, Args: args}, nil
}
