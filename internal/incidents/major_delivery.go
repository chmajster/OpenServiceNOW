package incidents

import (
	"context"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

// The stored update status describes outbox dispatch. The visible channel
// result comes from actual notification and SMTP delivery rows, including
// later retries; queueing is never presented as confirmed SMTP delivery.
func refreshMajorDelivery(ctx context.Context, e database.Executor, tenant uint64, items []MajorUpdate) error {
	if len(items) == 0 {
		return nil
	}
	marks := make([]string, len(items))
	args := []any{tenant}
	byID := map[uint64]*MajorUpdate{}
	for i := range items {
		marks[i] = "?"
		args = append(args, items[i].ID)
		byID[items[i].ID] = &items[i]
	}
	rows, err := e.QueryContext(ctx, `SELECT u.id,COALESCE(MAX(n.in_app_enabled),0),COALESCE(MAX(d.status IN('QUEUED','SENDING')),0),COALESCE(MAX(d.status='DELIVERED'),0),COALESCE(MAX(d.status='NOT_CONFIGURED'),0),COALESCE(MAX(d.status='FAILED'),0),COALESCE(MAX(d.error_code='SMTP_RESULT_UNKNOWN'),0) FROM incident_major_updates u LEFT JOIN notifications n ON n.tenant_id=u.tenant_id AND n.source_event_id=u.source_event_id LEFT JOIN notification_mail_deliveries d ON d.tenant_id=n.tenant_id AND d.notification_id=n.id WHERE u.tenant_id=? AND u.id IN(`+strings.Join(marks, ",")+`) GROUP BY u.id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uint64
		var inApp, queued, delivered, missing, failed, unknown bool
		if err = rows.Scan(&id, &inApp, &queued, &delivered, &missing, &failed, &unknown); err != nil {
			return err
		}
		item := byID[id]
		item.Delivery = majorDeliveryState(item.Delivery, inApp, queued, delivered, missing, failed, unknown)
	}
	return rows.Err()
}

func majorDeliveryState(dispatch string, inApp, queued, delivered, missing, failed, unknown bool) string {
	switch {
	case inApp:
		return "IN_APP_DELIVERED"
	case unknown:
		return "SMTP_UNKNOWN"
	case queued:
		return "SMTP_QUEUED"
	case delivered && (missing || failed):
		return "SMTP_PARTIAL"
	case delivered:
		return "SMTP_DELIVERED"
	case missing:
		return "SMTP_NOT_CONFIGURED"
	case failed:
		return "FAILED"
	default:
		return dispatch
	}
}
