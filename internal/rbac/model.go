package rbac

import (
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("rbac resource not found")
	ErrConflict = errors.New("rbac resource conflict")
	ErrInvalid  = errors.New("invalid rbac input")
	errNoChange = errors.New("rbac operation made no change")
)

type Permission struct {
	ID          uint64 `json:"id,string"`
	Key         string `json:"key"`
	Description string `json:"description,omitempty"`
}

type Role struct {
	ID          uint64       `json:"id,string"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	SystemRole  bool         `json:"system_role"`
	Version     uint64       `json:"version,string"`
	Permissions []Permission `json:"permissions"`
}

type Group struct {
	ID          uint64 `json:"id,string"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Version     uint64 `json:"version,string"`
	MemberCount uint64 `json:"member_count,string"`
}

type GroupMember struct {
	UserID           uint64    `json:"user_id,string"`
	Email            string    `json:"email"`
	DisplayName      string    `json:"display_name"`
	UserActive       bool      `json:"user_active"`
	MembershipActive bool      `json:"membership_active"`
	JoinedAt         time.Time `json:"joined_at"`
}

type AssignedRole struct {
	ID         uint64    `json:"id,string"`
	Name       string    `json:"name"`
	SystemRole bool      `json:"system_role"`
	AssignedAt time.Time `json:"assigned_at"`
}

type GroupMemberPage struct {
	Members     []GroupMember `json:"members"`
	NextAfterID uint64        `json:"next_after_id,string,omitempty"`
}

type AssignedRolePage struct {
	Roles       []AssignedRole `json:"roles"`
	NextAfterID uint64         `json:"next_after_id,string,omitempty"`
}

type CreateRoleInput struct {
	Name        string
	Description string
	Permissions []string
}

type CreateGroupInput struct {
	Name        string
	Description string
}

type RolePage struct {
	Roles       []Role `json:"roles"`
	NextAfterID uint64 `json:"next_after_id,string,omitempty"`
}

type GroupPage struct {
	Groups      []Group `json:"groups"`
	NextAfterID uint64  `json:"next_after_id,string,omitempty"`
}
