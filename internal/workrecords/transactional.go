package workrecords

import (
	"context"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

// Domain transactions reuse shared SQL; the domain supplies object authorization
// and records its own atomic audit/outbox business event.
func (s *Service) AppendMessageInTx(ctx context.Context, e database.Executor, a Actor, kind, id string, t MessageType, body string) (Message, error) {
	return s.repo.AppendMessage(ctx, e, a.TenantID, kind, id, t, body, a.UserID)
}
func (s *Service) AddWatcherInTx(ctx context.Context, e database.Executor, a Actor, kind, id string, userID uint64) (bool, error) {
	return s.repo.AddWatcher(ctx, e, a.TenantID, kind, id, userID, a.UserID)
}
func (s *Service) RemoveWatcherInTx(ctx context.Context, e database.Executor, a Actor, kind, id string, userID uint64) (bool, error) {
	return s.repo.RemoveWatcher(ctx, e, a.TenantID, kind, id, userID)
}
func (s *Service) AddRelationInTx(ctx context.Context, e database.Executor, a Actor, source, kind, target string) (Relation, bool, error) {
	return s.repo.AddRelation(ctx, e, a.TenantID, "incident", source, kind, "incident", target, a.UserID)
}
func (s *Service) RemoveRelationInTx(ctx context.Context, e database.Executor, a Actor, id uint64) (Relation, bool, error) {
	return s.repo.RemoveRelation(ctx, e, a.TenantID, id)
}
func (r *Repository) GetMessage(ctx context.Context, e database.Executor, tenantID, id uint64) (Message, error) {
	return r.getMessage(ctx, e, tenantID, id)
}
func (r *Repository) GetRelation(ctx context.Context, e database.Executor, tenantID, id uint64) (Relation, error) {
	return r.getRelation(ctx, e, tenantID, id)
}
