package tenants

import "context"

type APIService interface {
	Get(context.Context, uint64) (Tenant, error)
	UpdateName(context.Context, Actor, uint64, string) (Tenant, error)
}
