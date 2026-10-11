package identity

import (
	"context"
	"strings"
)

type SetupResult struct {
	TenantSlug string `json:"tenant"`
	AdminEmail string `json:"email"`
}

type SetupService interface {
	Required(context.Context) (bool, error)
	Create(context.Context, BootstrapInput, RequestMeta) (SetupResult, error)
}

type Installer struct {
	bootstrap *Bootstrapper
	auth      *Service
}

func NewInstaller(bootstrap *Bootstrapper, auth *Service) *Installer {
	return &Installer{bootstrap: bootstrap, auth: auth}
}

func (s *Installer) Required(ctx context.Context) (bool, error) {
	return s.bootstrap.repo.SetupRequired(ctx)
}

func (s *Installer) Create(ctx context.Context, input BootstrapInput, meta RequestMeta) (SetupResult, error) {
	err := s.auth.withPasswordCheck(func() error {
		_, err := s.bootstrap.run(ctx, input, true, meta)
		return err
	})
	if err != nil {
		return SetupResult{}, err
	}
	return SetupResult{
		TenantSlug: strings.ToLower(strings.TrimSpace(input.TenantSlug)),
		AdminEmail: normalizeLoginEmail(input.AdminEmail),
	}, nil
}
