package requests

import "github.com/chmajster/OpenServiceNOW/internal/workrecords"

func ReferenceScope(a Actor, tasks bool) (workrecords.ReferenceScope, error) {
	view := "all"
	if tasks {
		view = "group"
	}
	where, args, err := listWhere(a, tasks, "", "", view, false)
	s := workrecords.ReferenceScope{Kind: "request", Root: "requests", ID: "r.id", Number: "r.number", Title: "r.number", State: "r.state", Version: "r.version", CreatedAt: "r.created_at", UpdatedAt: "r.updated_at", GroupID: "NULL", GroupName: "''", AssignedTo: "NULL", ReporterID: "r.requester_id", Active: "r.state IN('SUBMITTED','IN_PROGRESS')", From: " FROM service_requests r", Where: where, Args: args}
	if tasks {
		s.Kind = "fulfillment_task"
		s.Root = "fulfillment"
		s.ID = "t.id"
		s.Number = "t.number"
		s.Title = "t.title"
		s.State = "t.state"
		s.Version = "t.version"
		s.UpdatedAt = "t.updated_at"
		s.GroupID = "wa.assignment_group_id"
		s.GroupName = "g.name"
		s.AssignedTo = "wa.assigned_to"
		s.From = taskJoins
		s.Active = "t.state NOT IN('COMPLETED','CANCELLED') AND r.state IN('SUBMITTED','IN_PROGRESS')"
	}
	return s, err
}

func RequestedItemReferenceScope(a Actor) (workrecords.ReferenceScope, error) {
	s, err := ReferenceScope(a, false)
	s.Kind = "requested_item"
	s.ID = "ri.id"
	s.Number = "ri.number"
	s.Title = "JSON_UNQUOTE(JSON_EXTRACT(ri.snapshot,'$.title'))"
	s.Parent = "r.id"
	s.ParentTab = "info"
	s.ParentQuery = "item_id"
	s.From += " JOIN requested_items ri ON ri.tenant_id=r.tenant_id AND ri.request_id=r.id"
	return s, err
}
