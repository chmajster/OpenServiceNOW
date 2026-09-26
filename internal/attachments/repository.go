package attachments

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type MetadataStore interface {
	Create(context.Context, CreateInput, string, string, uint64, [32]byte) (Attachment, error)
	Get(context.Context, uint64, uint64) (Attachment, error)
	BeginDelete(context.Context, uint64, uint64) (Attachment, error)
	RevertDelete(context.Context, uint64, uint64) error
	FinishDelete(context.Context, uint64, uint64) error
}

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(
	ctx context.Context,
	input CreateInput,
	storageKey string,
	contentType string,
	size uint64,
	sum [32]byte,
) (Attachment, error) {
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO attachments (
			tenant_id, object_type, object_id, file_name, content_type,
			size_bytes, sha256, storage_key, uploaded_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, input.TenantID, input.ObjectType, input.ObjectID, input.FileName, contentType,
		size, sum[:], storageKey, nullableUint64(input.UploadedBy))
	if err != nil {
		return Attachment{}, fmt.Errorf("create attachment metadata: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Attachment{}, fmt.Errorf("read attachment id: %w", err)
	}
	return r.Get(ctx, input.TenantID, uint64(id))
}

func (r *Repository) Get(ctx context.Context, tenantID, id uint64) (Attachment, error) {
	if tenantID == 0 || id == 0 {
		return Attachment{}, ErrInvalid
	}
	attachment, err := scanAttachment(r.db.QueryRowContext(ctx, attachmentSelect+`
		WHERE tenant_id = ? AND id = ?
	`, tenantID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Attachment{}, ErrNotFound
	}
	if err != nil {
		return Attachment{}, fmt.Errorf("get attachment: %w", err)
	}
	return attachment, nil
}

func (r *Repository) BeginDelete(ctx context.Context, tenantID, id uint64) (Attachment, error) {
	if tenantID == 0 || id == 0 {
		return Attachment{}, ErrInvalid
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE attachments
		SET status = 'DELETING'
		WHERE tenant_id = ? AND id = ? AND status = 'ACTIVE'
	`, tenantID, id)
	if err != nil {
		return Attachment{}, fmt.Errorf("begin attachment deletion: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Attachment{}, fmt.Errorf("read attachment delete result: %w", err)
	}
	if affected != 1 {
		var status Status
		if err := r.db.QueryRowContext(ctx, `
			SELECT status FROM attachments WHERE tenant_id = ? AND id = ?
		`, tenantID, id).Scan(&status); errors.Is(err, sql.ErrNoRows) {
			return Attachment{}, ErrNotFound
		} else if err != nil {
			return Attachment{}, fmt.Errorf("read attachment state: %w", err)
		}
		return Attachment{}, ErrConflict
	}
	return r.Get(ctx, tenantID, id)
}

func (r *Repository) RevertDelete(ctx context.Context, tenantID, id uint64) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE attachments
		SET status = 'ACTIVE'
		WHERE tenant_id = ? AND id = ? AND status = 'DELETING'
	`, tenantID, id)
	if err != nil {
		return fmt.Errorf("revert attachment deletion: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read attachment revert result: %w", err)
	}
	if affected != 1 {
		return ErrConflict
	}
	return nil
}

func (r *Repository) FinishDelete(ctx context.Context, tenantID, id uint64) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE attachments
		SET status = 'DELETED', deleted_at = ?
		WHERE tenant_id = ? AND id = ? AND status = 'DELETING'
	`, time.Now().UTC(), tenantID, id)
	if err != nil {
		return fmt.Errorf("finish attachment deletion: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read attachment finish result: %w", err)
	}
	if affected != 1 {
		return ErrConflict
	}
	return nil
}

const attachmentSelect = `
	SELECT
		id, tenant_id, object_type, object_id, file_name, content_type,
		size_bytes, sha256, storage_key, status, uploaded_by, created_at, deleted_at
	FROM attachments
`

type rowScanner interface {
	Scan(...any) error
}

func scanAttachment(row rowScanner) (Attachment, error) {
	var attachment Attachment
	var sum []byte
	var uploadedBy sql.Null[uint64]
	var deletedAt sql.NullTime
	if err := row.Scan(
		&attachment.ID,
		&attachment.TenantID,
		&attachment.ObjectType,
		&attachment.ObjectID,
		&attachment.FileName,
		&attachment.ContentType,
		&attachment.SizeBytes,
		&sum,
		&attachment.StorageKey,
		&attachment.Status,
		&uploadedBy,
		&attachment.CreatedAt,
		&deletedAt,
	); err != nil {
		return Attachment{}, err
	}
	if len(sum) != len(attachment.SHA256) {
		return Attachment{}, fmt.Errorf("invalid attachment checksum length %d", len(sum))
	}
	copy(attachment.SHA256[:], sum)
	if uploadedBy.Valid {
		value := uploadedBy.V
		attachment.UploadedBy = &value
	}
	if deletedAt.Valid {
		value := deletedAt.Time
		attachment.DeletedAt = &value
	}
	return attachment, nil
}

func nullableUint64(value *uint64) any {
	if value == nil {
		return nil
	}
	return *value
}
