package workrecords

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"regexp"
)

var ErrOperationConflict = errors.New("klucz ponowienia został użyty dla innej operacji lub treści")
var operationKey = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

func ReserveOperation(ctx context.Context, e database.Executor, a Actor, key, kind string, hash [32]byte) (uint64, error) {
	if !operationKey.MatchString(key) || len(kind) > 24 || kind == "" {
		return 0, ErrInvalid
	}
	if _, err := e.ExecContext(ctx, `INSERT INTO work_operation_keys(tenant_id,user_id,request_key,kind,request_hash) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE request_key=work_operation_keys.request_key`, a.TenantID, a.UserID, key, kind, hash[:]); err != nil {
		return 0, err
	}
	var stored []byte
	var storedKind string
	var id sql.Null[uint64]
	if err := e.QueryRowContext(ctx, "SELECT request_hash,kind,resource_id FROM work_operation_keys WHERE tenant_id=? AND user_id=? AND request_key=? FOR UPDATE", a.TenantID, a.UserID, key).Scan(&stored, &storedKind, &id); err != nil {
		return 0, err
	}
	if storedKind != kind || subtle.ConstantTimeCompare(stored, hash[:]) != 1 {
		return 0, ErrOperationConflict
	}
	return id.V, nil
}
func CompleteOperation(ctx context.Context, e database.Executor, a Actor, key string, id uint64) error {
	_, err := e.ExecContext(ctx, "UPDATE work_operation_keys SET resource_id=? WHERE tenant_id=? AND user_id=? AND request_key=?", id, a.TenantID, a.UserID, key)
	return err
}
