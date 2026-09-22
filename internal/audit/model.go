package audit

import "time"

type Event struct {
	TenantID    *uint64
	ActorUserID *uint64
	Time        time.Time
	Action      string
	ObjectType  string
	ObjectID    string
	OldValues   any
	NewValues   any
	RequestID   string
	Source      string
	IPAddress   string
}
