package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	platformdb "github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

type MySQLRepository struct{}

func NewMySQLRepository() *MySQLRepository {
	return &MySQLRepository{}
}

func (r *MySQLRepository) Append(ctx context.Context, exec platformdb.Executor, event Event) error {
	oldValues, err := marshalNullable(event.OldValues)
	if err != nil {
		return fmt.Errorf("marshal old audit values: %w", err)
	}
	newValues, err := marshalNullable(event.NewValues)
	if err != nil {
		return fmt.Errorf("marshal new audit values: %w", err)
	}
	eventTime := event.Time
	if eventTime.IsZero() {
		eventTime = time.Now().UTC()
	}

	_, err = exec.ExecContext(ctx, `
		INSERT INTO audit_events (
			tenant_id, actor_user_id, event_time, action, object_type, object_id,
			old_values, new_values, request_id, source, ip_address
		) VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, NULLIF(?, ''), ?, NULLIF(?, ''))
	`,
		event.TenantID,
		event.ActorUserID,
		eventTime,
		event.Action,
		event.ObjectType,
		event.ObjectID,
		oldValues,
		newValues,
		event.RequestID,
		event.Source,
		event.IPAddress,
	)
	if err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}
	return nil
}

func marshalNullable(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return string(encoded), nil
}
