package attachments

import (
	"context"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

type Page struct {
	Files        []Attachment
	NextBeforeID uint64
}

func (r *Repository) GetInTx(ctx context.Context, e database.Executor, tenantID, id uint64) (Attachment, error) {
	return r.get(ctx, e, tenantID, id)
}

func (r *Repository) ListVisible(ctx context.Context, tenantID uint64, objectType, objectID string, before uint64, limit int, internal bool) (Page, error) {
	if tenantID == 0 || objectType == "" || objectID == "" || limit < 1 || limit > 100 {
		return Page{}, ErrInvalid
	}
	q := attachmentSelect + ` WHERE tenant_id=? AND object_type=? AND object_id=? AND status='ACTIVE'`
	args := []any{tenantID, objectType, objectID}
	if !internal {
		q += " AND visibility='PUBLIC' AND (message_id IS NULL OR EXISTS(SELECT 1 FROM work_messages m WHERE m.tenant_id=attachments.tenant_id AND m.id=attachments.message_id AND m.object_type=attachments.object_type AND m.object_id=attachments.object_id AND m.message_type='COMMENT'))"
	}
	if before > 0 {
		q += " AND id<?"
		args = append(args, before)
	}
	q += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	p := Page{Files: []Attachment{}}
	for rows.Next() {
		v, err := scanAttachment(rows)
		if err != nil {
			return p, err
		}
		p.Files = append(p.Files, v)
	}
	if len(p.Files) > limit {
		p.Files = p.Files[:limit]
		p.NextBeforeID = p.Files[limit-1].ID
	}
	return p, rows.Err()
}

// MarkDeletedTx makes download inaccessible atomically with domain audit/outbox.
// Physical bytes are reconciled by durable cleanup, also after a crash.
func (r *Repository) MarkDeletedTx(ctx context.Context, e database.Executor, tenantID, id uint64) (bool, error) {
	result, err := e.ExecContext(ctx, `UPDATE attachments SET status='DELETED',deleted_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND status='ACTIVE'`, tenantID, id)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
