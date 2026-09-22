package tenants

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	platformdb "github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

type Service struct {
	repo  *MySQLRepository
	tx    *platformdb.TxManager
	audit *audit.MySQLRepository
}

func NewService(repo *MySQLRepository, tx *platformdb.TxManager, auditRepo *audit.MySQLRepository) *Service {
	return &Service{repo: repo, tx: tx, audit: auditRepo}
}

func (s *Service) Get(ctx context.Context, tenantID uint64) (Tenant, error) {
	return s.repo.Get(ctx, tenantID)
}

func (s *Service) UpdateName(
	ctx context.Context,
	actor Actor,
	expectedVersion uint64,
	name string,
) (Tenant, error) {
	name = strings.TrimSpace(name)
	if expectedVersion == 0 {
		return Tenant{}, fmt.Errorf("%w: version must be greater than zero", ErrInvalid)
	}
	if count := utf8.RuneCountInString(name); count < 1 || count > 200 {
		return Tenant{}, fmt.Errorf("%w: name must contain between 1 and 200 characters", ErrInvalid)
	}

	var updated Tenant
	err := s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec platformdb.Executor) error {
		current, err := s.repo.GetForUpdate(ctx, exec, actor.TenantID)
		if err != nil {
			return err
		}
		if current.Version != expectedVersion {
			return ErrConflict
		}
		if err := s.repo.UpdateName(ctx, exec, actor.TenantID, expectedVersion, name); err != nil {
			return err
		}
		updated, err = s.repo.GetForUpdate(ctx, exec, actor.TenantID)
		if err != nil {
			return err
		}

		return s.audit.Append(ctx, exec, audit.Event{
			TenantID:    &actor.TenantID,
			ActorUserID: &actor.UserID,
			Action:      "tenant.name.update",
			ObjectType:  "tenant",
			ObjectID:    fmt.Sprint(actor.TenantID),
			OldValues:   map[string]any{"name": current.Name, "version": current.Version},
			NewValues:   map[string]any{"name": name, "version": updated.Version},
			RequestID:   actor.RequestID,
			Source:      "api",
			IPAddress:   actor.IPAddress,
		})
	})
	if err != nil {
		return Tenant{}, err
	}
	return updated, nil
}
