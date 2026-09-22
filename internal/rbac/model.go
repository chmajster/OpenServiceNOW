package rbac

import "errors"

var (
	ErrNotFound = errors.New("rbac resource not found")
	ErrConflict = errors.New("rbac resource conflict")
	ErrInvalid  = errors.New("invalid rbac input")
)

type Permission struct {
	ID          uint64 `json:"id"`
	Key         string `json:"key"`
	Description string `json:"description,omitempty"`
}

type Role struct {
	ID          uint64       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	SystemRole  bool         `json:"system_role"`
	Version     uint64       `json:"version"`
	Permissions []Permission `json:"permissions"`
}

type Group struct {
	ID          uint64 `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Version     uint64 `json:"version"`
	MemberCount uint64 `json:"member_count"`
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
