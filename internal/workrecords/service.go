package workrecords

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

type Service struct {
	repo *Repository
	tx *database.TxManager
	outbox *events.Repository
}

func NewService(
	repo *Repository,
	tx *database.TxManager,
	outbox *events.Repository,
) *Service {
	return &Service{repo: repo, tx: tx, outbox: outbox}
}

func (s *Service) SetAssignment(
	ctx context.Context,
	actor Actor,
	objectType, objectID string,
	expectedVersion uint64,
	assignedTo, groupID *uint64,
) (Assignment, error) {
	objectType, objectID = normalizeObjectRef(objectType, objectID)
	if !validActor(actor) || !validObjectRef(actor.TenantID, objectType, objectID) {
		return Assignment{}, ErrInvalid
	}
	var updated Assignment
	err := s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec database.Executor) error {
		change, err := s.repo.SetAssignment(
			ctx, exec, actor.TenantID, objectType, objectID, expectedVersion, assignedTo, groupID,
		)
		if err != nil {
			return err
		}
		updated = change.After
		if !change.Changed {
			return nil
		}
		payload, err := json.Marshal(AssignmentEventPayload{
			ActorUserID: actor.UserID,
			AssignedTo: updated.AssignedTo,
			AssignmentGroupID: updated.AssignmentGroupID,
			Version: updated.Version,
		})
		if err != nil {
			return fmt.Errorf("marshal assignment event: %w", err)
		}
		_, _, err = s.outbox.Append(ctx, exec, events.Input{
			TenantID: actor.TenantID,
			Type: "work.assignment.changed",
			AggregateType: objectType,
			AggregateID: objectID,
			Payload: payload,
			IdempotencyKey: eventKey("assignment", objectType, objectID, fmt.Sprint(updated.Version)),
		})
		return err
	})
	return updated, err
}

func (s *Service) AppendMessage(
	ctx context.Context,
	actor Actor,
	objectType, objectID string,
	messageType MessageType,
	body string,
) (Message, error) {
	objectType, objectID = normalizeObjectRef(objectType, objectID)
	if !validActor(actor) || !validObjectRef(actor.TenantID, objectType, objectID) {
		return Message{}, ErrInvalid
	}
	var message Message
	err := s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec database.Executor) error {
		var err error
		message, err = s.repo.AppendMessage(
			ctx, exec, actor.TenantID, objectType, objectID, messageType, body, actor.UserID,
		)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(MessageEventPayload{
			ActorUserID: actor.UserID,
			MessageID: message.ID,
			MessageType: message.Type,
			Body: message.Body,
		})
		if err != nil {
			return fmt.Errorf("marshal message event: %w", err)
		}
		eventType := "work.comment.added"
		if messageType == MessageWorkNote {
			eventType = "work.work_note.added"
		}
		_, _, err = s.outbox.Append(ctx, exec, events.Input{
			TenantID: actor.TenantID,
			Type: eventType,
			AggregateType: objectType,
			AggregateID: objectID,
			Payload: payload,
			IdempotencyKey: eventKey("message", fmt.Sprint(message.ID)),
		})
		return err
	})
	return message, err
}

func (s *Service) AddWatcher(
	ctx context.Context,
	actor Actor,
	objectType, objectID string,
	userID uint64,
) (bool, error) {
	objectType, objectID = normalizeObjectRef(objectType, objectID)
	if !validActor(actor) || userID == 0 || !validObjectRef(actor.TenantID, objectType, objectID) {
		return false, ErrInvalid
	}
	created := false
	err := s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec database.Executor) error {
		var err error
		created, err = s.repo.AddWatcher(ctx, exec, actor.TenantID, objectType, objectID, userID, actor.UserID)
		if err != nil || !created {
			return err
		}
		payload, err := json.Marshal(WatcherEventPayload{ActorUserID: actor.UserID, UserID: userID})
		if err != nil {
			return fmt.Errorf("marshal watcher event: %w", err)
		}
		_, _, err = s.outbox.Append(ctx, exec, events.Input{
			TenantID: actor.TenantID,
			Type: "work.watcher.added",
			AggregateType: objectType,
			AggregateID: objectID,
			Payload: payload,
		})
		return err
	})
	return created, err
}

func (s *Service) RemoveWatcher(
	ctx context.Context,
	actor Actor,
	objectType, objectID string,
	userID uint64,
) (bool, error) {
	objectType, objectID = normalizeObjectRef(objectType, objectID)
	if !validActor(actor) || userID == 0 || !validObjectRef(actor.TenantID, objectType, objectID) {
		return false, ErrInvalid
	}
	removed := false
	err := s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec database.Executor) error {
		var err error
		removed, err = s.repo.RemoveWatcher(ctx, exec, actor.TenantID, objectType, objectID, userID)
		if err != nil || !removed {
			return err
		}
		payload, err := json.Marshal(WatcherEventPayload{ActorUserID: actor.UserID, UserID: userID})
		if err != nil {
			return fmt.Errorf("marshal watcher removal event: %w", err)
		}
		_, _, err = s.outbox.Append(ctx, exec, events.Input{
			TenantID: actor.TenantID,
			Type: "work.watcher.removed",
			AggregateType: objectType,
			AggregateID: objectID,
			Payload: payload,
		})
		return err
	})
	return removed, err
}


func (s *Service) AddRelation(
	ctx context.Context,
	actor Actor,
	sourceType, sourceID, relationType, targetType, targetID string,
) (Relation, bool, error) {
	sourceType, sourceID = normalizeObjectRef(sourceType, sourceID)
	targetType, targetID = normalizeObjectRef(targetType, targetID)
	relationType = strings.TrimSpace(relationType)
	if !validActor(actor) ||
		!validObjectRef(actor.TenantID, sourceType, sourceID) ||
		!validObjectRef(actor.TenantID, targetType, targetID) {
		return Relation{}, false, ErrInvalid
	}
	var relation Relation
	created := false
	err := s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec database.Executor) error {
		var err error
		relation, created, err = s.repo.AddRelation(
			ctx, exec, actor.TenantID, sourceType, sourceID, relationType, targetType, targetID, actor.UserID,
		)
		if err != nil || !created {
			return err
		}
		payload, err := json.Marshal(RelationEventPayload{
			ActorUserID: actor.UserID,
			RelationID: relation.ID,
			RelationType: relation.Type,
			TargetType: relation.TargetType,
			TargetID: relation.TargetID,
		})
		if err != nil {
			return fmt.Errorf("marshal relation event: %w", err)
		}
		_, _, err = s.outbox.Append(ctx, exec, events.Input{
			TenantID: actor.TenantID,
			Type: "work.relation.added",
			AggregateType: sourceType,
			AggregateID: sourceID,
			Payload: payload,
			IdempotencyKey: eventKey("relation", fmt.Sprint(relation.ID)),
		})
		return err
	})
	return relation, created, err
}

func (s *Service) RemoveRelation(
	ctx context.Context,
	actor Actor,
	relationID uint64,
) (bool, error) {
	if !validActor(actor) || relationID == 0 {
		return false, ErrInvalid
	}
	removed := false
	err := s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec database.Executor) error {
		relation, didRemove, err := s.repo.RemoveRelation(ctx, exec, actor.TenantID, relationID)
		if err != nil || !didRemove {
			return err
		}
		removed = true
		payload, err := json.Marshal(RelationEventPayload{
			ActorUserID: actor.UserID,
			RelationID: relation.ID,
			RelationType: relation.Type,
			TargetType: relation.TargetType,
			TargetID: relation.TargetID,
		})
		if err != nil {
			return fmt.Errorf("marshal relation removal event: %w", err)
		}
		_, _, err = s.outbox.Append(ctx, exec, events.Input{
			TenantID: actor.TenantID,
			Type: "work.relation.removed",
			AggregateType: relation.SourceType,
			AggregateID: relation.SourceID,
			Payload: payload,
			IdempotencyKey: eventKey("relation-removed", fmt.Sprint(relation.ID)),
		})
		return err
	})
	return removed, err
}


func validActor(actor Actor) bool {
	return actor.TenantID > 0 && actor.UserID > 0
}

func normalizeObjectRef(objectType, objectID string) (string, string) {
	return strings.TrimSpace(objectType), strings.TrimSpace(objectID)
}

func eventKey(parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(hash[:])
}
