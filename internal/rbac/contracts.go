package rbac

import "context"

type APIService interface {
	ListPermissions(context.Context) ([]Permission, error)
	ListRoles(context.Context, uint64) ([]Role, error)
	CreateRole(context.Context, Actor, CreateRoleInput) (uint64, error)
	SetRolePermissions(context.Context, Actor, uint64, []string) error
	ListGroups(context.Context, uint64) ([]Group, error)
	CreateGroup(context.Context, Actor, CreateGroupInput) (uint64, error)
	AddGroupMember(context.Context, Actor, uint64, uint64) error
	RemoveGroupMember(context.Context, Actor, uint64, uint64) error
	AssignRoleToUser(context.Context, Actor, uint64, uint64) error
	RemoveRoleFromUser(context.Context, Actor, uint64, uint64) error
	AssignRoleToGroup(context.Context, Actor, uint64, uint64) error
	RemoveRoleFromGroup(context.Context, Actor, uint64, uint64) error
}
