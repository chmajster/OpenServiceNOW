package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	platformdb "github.com/chmajster/OpenServiceNOW/internal/platform/database"
	mysqlDriver "github.com/go-sql-driver/mysql"
)

type MySQLRepository struct {
	db *sql.DB
}

func NewMySQLRepository(db *sql.DB) *MySQLRepository {
	return &MySQLRepository{db: db}
}

func (r *MySQLRepository) List(
	ctx context.Context,
	tenantID, afterID uint64,
	limit int,
	query string,
) (Page, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			u.id, u.email, u.display_name, u.active, tm.active, u.created_at
		FROM tenant_memberships tm
		JOIN users u ON u.id = tm.user_id
		WHERE tm.tenant_id = ?
		  AND u.id > ?
		  AND (
			? = ''
			OR LOCATE(?, u.email) > 0
			OR LOCATE(?, u.display_name) > 0
		  )
		ORDER BY u.id
		LIMIT ?
	`, tenantID, afterID, query, query, query, limit+1)
	if err != nil {
		return Page{}, fmt.Errorf("list tenant users: %w", err)
	}
	defer rows.Close()

	result := make([]User, 0, limit+1)
	for rows.Next() {
		var user User
		if err := rows.Scan(
			&user.ID,
			&user.Email,
			&user.DisplayName,
			&user.UserActive,
			&user.MembershipActive,
			&user.CreatedAt,
		); err != nil {
			return Page{}, fmt.Errorf("scan tenant user: %w", err)
		}
		result = append(result, user)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("iterate tenant users: %w", err)
	}

	page := Page{Users: result}
	if len(result) > limit {
		page.Users = result[:limit]
		page.NextAfterID = page.Users[len(page.Users)-1].ID
	}
	return page, nil
}

func (r *MySQLRepository) CreateLocal(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID uint64,
	email, displayName, passwordHash string,
) (uint64, error) {
	result, err := exec.ExecContext(ctx, `
		INSERT INTO users (email, display_name, password_hash, active)
		VALUES (?, ?, ?, TRUE)
	`, email, displayName, passwordHash)
	if err != nil {
		if isDuplicate(err) {
			return 0, ErrConflict
		}
		return 0, fmt.Errorf("create local user: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read created user id: %w", err)
	}
	userID := uint64(id)

	if _, err := exec.ExecContext(ctx, `
		INSERT INTO tenant_memberships (tenant_id, user_id, active)
		VALUES (?, ?, TRUE)
	`, tenantID, userID); err != nil {
		return 0, fmt.Errorf("create tenant membership: %w", err)
	}
	return userID, nil
}

func (r *MySQLRepository) AddExistingMembership(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID uint64,
	email string,
) (uint64, error) {
	var userID uint64
	err := exec.QueryRowContext(ctx, `
		SELECT id
		FROM users
		WHERE email = ? AND active = TRUE
		FOR UPDATE
	`, email).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("find existing active user: %w", err)
	}

	_, err = exec.ExecContext(ctx, `
		INSERT INTO tenant_memberships (tenant_id, user_id, active)
		VALUES (?, ?, TRUE)
	`, tenantID, userID)
	if err != nil {
		if isDuplicate(err) {
			return 0, ErrConflict
		}
		return 0, fmt.Errorf("add existing tenant membership: %w", err)
	}
	return userID, nil
}

func (r *MySQLRepository) SetMembershipActive(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID, userID uint64,
	active bool,
) error {
	result, err := exec.ExecContext(ctx, `
		UPDATE tenant_memberships
		SET active = ?
		WHERE tenant_id = ? AND user_id = ?
	`, active, tenantID, userID)
	if err != nil {
		return fmt.Errorf("update tenant membership: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read tenant membership update result: %w", err)
	}
	if affected == 0 {
		var exists bool
		if err := exec.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM tenant_memberships
				WHERE tenant_id = ? AND user_id = ?
			)
		`, tenantID, userID).Scan(&exists); err != nil {
			return fmt.Errorf("verify tenant membership: %w", err)
		}
		if !exists {
			return ErrNotFound
		}
	}
	return nil
}

func isDuplicate(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

func (r *MySQLRepository) MembershipActiveForUpdate(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID, userID uint64,
) (bool, error) {
	var active bool
	err := exec.QueryRowContext(ctx, `
		SELECT active
		FROM tenant_memberships
		WHERE tenant_id = ? AND user_id = ?
		FOR UPDATE
	`, tenantID, userID).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("read tenant membership state: %w", err)
	}
	return active, nil
}
