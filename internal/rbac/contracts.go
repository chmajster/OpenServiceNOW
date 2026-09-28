package rbac

import "context"

type APIService interface {
	ListPermissions(context.Context) ([]Permission, error)
	ListRoles(context.Context, uint64, uint64, int, string) (RolePage, error)
	CreateRole(context.Context, Actor, CreateRoleInput) (uint64, error)
	SetRolePermissions(context.Context, Actor, uint64, uint64, []string) error
	ListGroups(context.Context, uint64, uint64, int, string) (GroupPage, error)
	ListGroupMembers(context.Context, uint64, uint64, uint64, int) (GroupMemberPage, error)
	ListUserRoles(context.Context, uint64, uint64, uint64, int) (AssignedRolePage, error)
	ListGroupRoles(context.Context, uint64, uint64, uint64, int) (AssignedRolePage, error)
	CreateGroup(context.Context, Actor, CreateGroupInput) (uint64, error)
	AddGroupMember(context.Context, Actor, uint64, uint64) error
	RemoveGroupMember(context.Context, Actor, uint64, uint64) error
	AssignRoleToUser(context.Context, Actor, uint64, uint64) error
	RemoveRoleFromUser(context.Context, Actor, uint64, uint64) error
	AssignRoleToGroup(context.Context, Actor, uint64, uint64) error
	RemoveRoleFromGroup(context.Context, Actor, uint64, uint64) error
}
