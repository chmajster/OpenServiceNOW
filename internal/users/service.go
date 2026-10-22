package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	platformdb "github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/platform/password"
)

type PasswordHasher interface{ HashPassword(string) (string, error) }

type Service struct {
	hasher       PasswordHasher
	repo         *MySQLRepository
	tx           *platformdb.TxManager
	audit        *audit.MySQLRepository
	adminSafety  *identity.AdminSafetyRepository
	identityRepo *identity.MySQLRepository
}

func NewService(
	repo *MySQLRepository,
	tx *platformdb.TxManager,
	auditRepo *audit.MySQLRepository,
	adminSafety *identity.AdminSafetyRepository,
	identityRepo *identity.MySQLRepository,
	hasher PasswordHasher,
) *Service {
	return &Service{
		hasher:       hasher,
		repo:         repo,
		tx:           tx,
		audit:        auditRepo,
		adminSafety:  adminSafety,
		identityRepo: identityRepo,
	}
}

func (s *Service) List(
	ctx context.Context,
	tenantID, afterID uint64,
	limit int,
	query string,
) (Page, error) {
	if limit < 1 || limit > 200 {
		return Page{}, fmt.Errorf("%w: limit must be between 1 and 200", ErrInvalid)
	}
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) > 200 {
		return Page{}, fmt.Errorf("%w: query exceeds 200 characters", ErrInvalid)
	}
	return s.repo.List(ctx, tenantID, afterID, limit, query)
}

func (s *Service) Profile(ctx context.Context, tenantID, userID uint64) (Profile, error) {
	return s.repo.Profile(ctx, tenantID, userID)
}

func (s *Service) Create(ctx context.Context, actor Actor, input CreateInput) (uint64, error) {
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.DisplayName = strings.TrimSpace(input.DisplayName)

	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Address != input.Email || len(input.Email) > 254 {
		return 0, fmt.Errorf("%w: invalid email", ErrInvalid)
	}
	nameLength := utf8.RuneCountInString(input.DisplayName)
	if !utf8.ValidString(input.DisplayName) || nameLength < 1 || nameLength > 200 {
		return 0, fmt.Errorf("%w: display name must contain between 1 and 200 characters", ErrInvalid)
	}

	if err := password.Validate(input.Password); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	passwordHash, err := s.hasher.HashPassword(input.Password)
	if err != nil {
		return 0, err
	}

	var userID uint64
	err = s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec platformdb.Executor) error {
		var createErr error
		userID, createErr = s.repo.CreateLocal(
			ctx, exec, actor.TenantID, input.Email, input.DisplayName, passwordHash,
		)
		if createErr != nil {
			return createErr
		}
		return s.audit.Append(ctx, exec, audit.Event{
			TenantID:    &actor.TenantID,
			ActorUserID: &actor.UserID,
			Action:      "user.local.create",
			ObjectType:  "user",
			ObjectID:    fmt.Sprint(userID),
			NewValues: map[string]any{
				"email":        input.Email,
				"display_name": input.DisplayName,
			},
			RequestID: actor.RequestID,
			Source:    "api",
			IPAddress: actor.IPAddress,
		})
	})
	if err != nil {
		return 0, err
	}
	return userID, nil
}

func (s *Service) AddExistingMembership(
	ctx context.Context,
	actor Actor,
	input AddExistingInput,
) (uint64, error) {
	email := strings.ToLower(strings.TrimSpace(input.Email))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 {
		return 0, fmt.Errorf("%w: invalid email", ErrInvalid)
	}

	var userID uint64
	err = s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec platformdb.Executor) error {
		if err := s.adminSafety.LockTenant(ctx, exec, actor.TenantID); err != nil {
			return err
		}
		var addErr error
		userID, addErr = s.repo.AddExistingMembership(ctx, exec, actor.TenantID, email)
		if addErr != nil {
			return addErr
		}
		return s.audit.Append(ctx, exec, audit.Event{
			TenantID:    &actor.TenantID,
			ActorUserID: &actor.UserID,
			Action:      "user.membership.add",
			ObjectType:  "user",
			ObjectID:    fmt.Sprint(userID),
			NewValues:   map[string]any{"email": email, "membership_active": true},
			RequestID:   actor.RequestID,
			Source:      "api",
			IPAddress:   actor.IPAddress,
		})
	})
	if err != nil {
		return 0, err
	}
	return userID, nil
}

func (s *Service) SetMembershipActive(ctx context.Context, actor Actor, userID uint64, active bool) error {
	if actor.UserID == userID && !active {
		return fmt.Errorf("%w: cannot disable own tenant membership", ErrInvalid)
	}
	return s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec platformdb.Executor) error {
		if err := s.adminSafety.LockTenant(ctx, exec, actor.TenantID); err != nil {
			return err
		}
		if err := s.identityRepo.LockMembershipUser(ctx, exec, actor.TenantID, userID); err != nil {
			if errors.Is(err, identity.ErrNotFound) {
				return ErrNotFound
			}
			return err
		}
		current, err := s.repo.MembershipActiveForUpdate(ctx, exec, actor.TenantID, userID)
		if err != nil {
			return err
		}
		if current == active {
			return nil
		}

		systemRoleBinding, err := s.adminSafety.HasSystemRoleBinding(
			ctx,
			exec,
			actor.TenantID,
			userID,
		)
		if err != nil {
			return err
		}
		if systemRoleBinding {
			allowed, err := s.adminSafety.IsEffectivePlatformAdmin(
				ctx,
				exec,
				actor.TenantID,
				actor.UserID,
			)
			if err != nil {
				return err
			}
			if !allowed {
				return identity.ErrPlatformAdminRequired
			}
		}

		if err := s.repo.SetMembershipActive(ctx, exec, actor.TenantID, userID, active); err != nil {
			return err
		}
		if !active {
			count, err := s.adminSafety.CountEffectivePlatformAdmins(ctx, exec, actor.TenantID)
			if err != nil {
				return err
			}
			if count == 0 {
				return identity.ErrLastPlatformAdmin
			}
			if _, err := s.identityRepo.RevokeUserSessions(ctx, exec, actor.TenantID, userID); err != nil {
				return err
			}
		}
		return s.audit.Append(ctx, exec, audit.Event{
			TenantID:    &actor.TenantID,
			ActorUserID: &actor.UserID,
			Action:      "user.membership.update",
			ObjectType:  "user",
			ObjectID:    fmt.Sprint(userID),
			NewValues:   map[string]any{"membership_active": active},
			RequestID:   actor.RequestID,
			Source:      "api",
			IPAddress:   actor.IPAddress,
		})
	})
}
