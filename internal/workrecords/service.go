package workrecords

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

type Service struct {
	repo *Repository
	tx *database.TxManager
	activity *activity.Repository
	outbox *events.Repository
}

func NewService(
	repo *Repository,
	tx *database.TxManager,
	activityRepo *activity.Repository,
	outbox *events.Repository,
) *Service {
	return &Service{repo: repo, tx: tx, activity: activityRepo, outbox: outbox}
}

func (s *Service) SetAssignment(
	ctx context.Context,
	actor Actor,
	objectType, objectID string,
	expectedVersion uint64,
	assignedTo, groupID *uint64,
) (Assignment, error) {
	if !validActor(actor) {
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
		payload, err := json.Marshal(AssignmentEventPayload{
			AssignedTo: updated.AssignedTo,
			AssignmentGroupID: updated.AssignmentGroupID,
			Version: updated.Version,
		})
		if err != nil {
			return fmt.Errorf("marshal assignment event: %w", err)
		}
		eventID, _, err := s.outbox.Append(ctx, exec, events.Input{
			TenantID: actor.TenantID,
			Type: "work.assignment.changed",
			AggregateType: objectType,
			AggregateID: objectID,
			Payload: payload,
			IdempotencyKey: fmt.Sprintf("assignment:%s:%s:v%d", objectType, objectID, updated.Version),
		})
		if err != nil {
			return err
		}
		_, _, err = s.activity.Append(ctx, exec, activity.Input{
			TenantID: actor.TenantID,
			ObjectType: objectType,
			ObjectID: objectID,
			Type: "assignment",
			ActorUserID: &actor.UserID,
			Summary: "Assignment updated",
			Data: payload,
			SourceEventID: &eventID,
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
	if !validActor(actor) {
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
			MessageID: message.ID,
			MessageType: message.Type,
			Body: message.Body,
			AuthorUserID: message.AuthorUserID,
		})
		if err != nil {
			return fmt.Errorf("marshal message event: %w", err)
		}
		eventType := "work.comment.added"
		summary := "Comment added"
		activityType := "comment"
		if messageType == MessageWorkNote {
			eventType = "work.work_note.added"
			summary = "Work note added"
			activityType = "work_note"
		}
		eventID, _, err := s.outbox.Append(ctx, exec, events.Input{
			TenantID: actor.TenantID,
			Type: eventType,
			AggregateType: objectType,
			AggregateID: objectID,
			Payload: payload,
			IdempotencyKey: fmt.Sprintf("message:%d", message.ID),
		})
		if err != nil {
			return err
		}
		_, _, err = s.activity.Append(ctx, exec, activity.Input{
			TenantID: actor.TenantID,
			ObjectType: objectType,
			ObjectID: objectID,
			Type: activityType,
			ActorUserID: &actor.UserID,
			Summary: summary,
			Data: payload,
			SourceEventID: &eventID,
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
	if !validActor(actor) || userID == 0 {
		return false, ErrInvalid
	}
	created := false
	err := s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec database.Executor) error {
		var err error
		created, err = s.repo.AddWatcher(ctx, exec, actor.TenantID, objectType, objectID, userID, actor.UserID)
		if err != nil || !created {
			return err
		}
		payload, _ := json.Marshal(WatcherEventPayload{UserID: userID})
		eventID, _, err := s.outbox.Append(ctx, exec, events.Input{
			TenantID: actor.TenantID,
			Type: "work.watcher.added",
			AggregateType: objectType,
			AggregateID: objectID,
			Payload: payload,
			IdempotencyKey: fmt.Sprintf("watcher:%s:%s:%d", objectType, objectID, userID),
		})
		if err != nil {
			return err
		}
		_, _, err = s.activity.Append(ctx, exec, activity.Input{
			TenantID: actor.TenantID,
			ObjectType: objectType,
			ObjectID: objectID,
			Type: "watcher",
			ActorUserID: &actor.UserID,
			Summary: "Watcher added",
			Data: payload,
			SourceEventID: &eventID,
		})
		return err
	})
	return created, err
}

func (s *Service) AddRelation(
	ctx context.Context,
	actor Actor,
	sourceType, sourceID, relationType, targetType, targetID string,
) (Relation, bool, error) {
	if !validActor(actor) {
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
		payload, _ := json.Marshal(RelationEventPayload{
			RelationID: relation.ID,
			RelationType: relation.Type,
			TargetType: relation.TargetType,
			TargetID: relation.TargetID,
		})
		eventID, _, err := s.outbox.Append(ctx, exec, events.Input{
			TenantID: actor.TenantID,
			Type: "work.relation.added",
			AggregateType: sourceType,
			AggregateID: sourceID,
			Payload: payload,
			IdempotencyKey: fmt.Sprintf("relation:%d", relation.ID),
		})
		if err != nil {
			return err
		}
		_, _, err = s.activity.Append(ctx, exec, activity.Input{
			TenantID: actor.TenantID,
			ObjectType: sourceType,
			ObjectID: sourceID,
			Type: "relation",
			ActorUserID: &actor.UserID,
			Summary: "Related record added",
			Data: payload,
			SourceEventID: &eventID,
		})
		return err
	})
	return relation, created, err
}

func validActor(actor Actor) bool {
	return actor.TenantID > 0 && actor.UserID > 0
}
