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

func (r *MySQLRepository) List(ctx context.Context, tenantID uint64) ([]User, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			u.id, u.email, u.display_name, u.active, tm.active, u.created_at
		FROM tenant_memberships tm
		JOIN users u ON u.id = tm.user_id
		WHERE tm.tenant_id = ?
		ORDER BY u.display_name, u.email
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list tenant users: %w", err)
	}
	defer rows.Close()

	result := make([]User, 0)
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
			return nil, fmt.Errorf("scan tenant user: %w", err)
		}
		result = append(result, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenant users: %w", err)
	}
	return result, nil
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
