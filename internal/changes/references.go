package changes

import "github.com/chmajster/OpenServiceNOW/internal/workrecords"

func ReferenceScope(a Actor, tasks bool) (workrecords.ReferenceScope, error) {
	where, args, err := listWhere(a, Filter{})
	s := workrecords.ReferenceScope{Kind: "change", Root: "changes", ID: "p.id", Number: "p.number", Title: "p.title", State: "p.state", Version: "p.version", CreatedAt: "p.created_at", UpdatedAt: "p.updated_at", GroupID: "wa.assignment_group_id", GroupName: "COALESCE(g.name,'')", AssignedTo: "wa.assigned_to", ReporterID: "p.reporter_id", Active: "p.state NOT IN('CLOSED','CANCELLED')", From: joins, Where: where, Args: args}
	if tasks {
		s.Kind = "change_task"
		s.ID = "t.id"
		s.Number = "t.number"
		s.Title = "t.title"
		s.State = "t.state"
		s.Version = "t.version"
		s.Parent = "p.id"
		s.CreatedAt = "t.created_at"
		s.UpdatedAt = "t.updated_at"
		s.From += " JOIN change_tasks t ON t.tenant_id=p.tenant_id AND t.change_id=p.id"
		s.Active = "t.state NOT IN('COMPLETED','CANCELLED') AND (" + s.Active + ")"
	}
	return s, err
}
