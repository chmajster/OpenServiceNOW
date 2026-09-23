package users

import "context"

type APIService interface {
	List(context.Context, uint64, uint64, int, string) (Page, error)
	Create(context.Context, Actor, CreateInput) (uint64, error)
	SetMembershipActive(context.Context, Actor, uint64, bool) error
}
