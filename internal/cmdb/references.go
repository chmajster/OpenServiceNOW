package cmdb

import "github.com/chmajster/OpenServiceNOW/internal/workrecords"

func ReferenceScope(a Actor) (workrecords.ReferenceScope, error) {
	where, args, err := listWhere(a, Filter{})
	return workrecords.ReferenceScope{Class: "c.class", Kind: "ci", Root: "cmdb", ID: "c.id", Number: "c.identifier", Title: "c.name", State: "c.status", Version: "c.version", CreatedAt: "c.created_at", UpdatedAt: "c.updated_at", GroupID: "c.support_group_id", GroupName: "COALESCE(g.name,'')", AssignedTo: "c.owner_id", ReporterID: "c.owner_id", Active: "c.archived=FALSE", From: ciJoins, Where: where, Args: args}, err
}
