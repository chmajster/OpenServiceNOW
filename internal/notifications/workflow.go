package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workflow"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"strconv"
	"time"
)

func (s *Service) handleWorkflow(ctx context.Context, event events.Event) error {
	var p struct {
		ID   uint64    `json:"execution_id,string"`
		Node string    `json:"node_id"`
		At   time.Time `json:"occurred_at"`
	}
	request, err := strconv.ParseUint(event.AggregateID, 10, 64)
	if err != nil || request == 0 || event.TenantID == 0 || event.AggregateType != "request" || event.Version != 1 || json.Unmarshal(event.Payload, &p) != nil || p.ID == 0 || p.At.IsZero() {
		return events.ErrInvalid
	}
	return s.tx.WithTx(ctx, nil, func(e database.Executor) error {
		var tenantState string
		if err := e.QueryRowContext(ctx, "SELECT status FROM tenants WHERE id=? FOR UPDATE", event.TenantID).Scan(&tenantState); err != nil {
			return err
		}
		if tenantState != "ACTIVE" {
			return nil
		}
		var requester uint64
		if err := e.QueryRowContext(ctx, "SELECT requester_id FROM workflow_executions WHERE tenant_id=? AND id=? AND request_id=?", event.TenantID, p.ID, request).Scan(&requester); err != nil {
			return err
		}
		users := []uint64{requester}
		label := "Zaktualizowano proces Twojego wniosku"
		kind := "state"
		if event.Type == "workflow.approval.requested" {
			label = "Wniosek oczekuje na Twoją akceptację"
			kind = "approval"
			users = []uint64{}
			rows, err := e.QueryContext(ctx, "SELECT user_id FROM workflow_decisions WHERE tenant_id=? AND execution_id=? AND node_id=? ORDER BY user_id LIMIT 100", event.TenantID, p.ID, p.Node)
			if err != nil {
				return err
			}
			for rows.Next() {
				var id uint64
				if err = rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				users = append(users, id)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
		}
		for _, user := range users {
			a, err := s.currentActor(ctx, e, event.TenantID, user)
			if err != nil {
				return err
			}
			predicate, args := workflow.AccessSQL(workrecords.Principal{Actor: workrecords.Actor{TenantID: event.TenantID, UserID: user}, Permissions: a.Permissions}, "x")
			params := []any{event.TenantID, p.ID}
			params = append(params, args...)
			var allowed bool
			if err = e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM workflow_executions x WHERE x.tenant_id=? AND x.id=? AND "+predicate+")", params...).Scan(&allowed); err != nil {
				return err
			}
			if !allowed {
				continue
			}
			prefs, err := s.preference(ctx, e, event.TenantID, user)
			if err != nil {
				return err
			}
			if !prefs.StateChanges || !prefs.InApp && !prefs.SMTP {
				continue
			}
			result, err := e.ExecContext(ctx, "INSERT INTO notifications(tenant_id,user_id,source_event_id,incident_id,execution_id,kind,visibility,summary,in_app_enabled,occurred_at) VALUES(?,?,?,NULL,?,?,'PUBLIC',?,?,?) ON DUPLICATE KEY UPDATE id=id", event.TenantID, user, event.ID, p.ID, kind, label, prefs.InApp, p.At)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n == 0 {
				continue
			}
			id, err := result.LastInsertId()
			if err != nil {
				return err
			}
			if prefs.SMTP {
				if _, err = s.createMail(ctx, e, event.TenantID, user, uint64(id)); err != nil {
					return fmt.Errorf("workflow SMTP job: %w", err)
				}
			}
		}
		return nil
	})
}
