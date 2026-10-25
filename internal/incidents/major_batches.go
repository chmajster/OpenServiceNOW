package incidents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

// MajorChildren checkpoints each independent outcome. A transport retry returns
// the original successful/failed outcomes, even after a later parent update.
func (s *Service) MajorChildren(ctx context.Context, a Actor, parent uint64, input MajorChildrenInput, key string) ([]MajorChildResult, error) {
	if !a.Has("major.manage") {
		return nil, ErrForbidden
	}
	input.Message = strings.TrimSpace(input.Message)
	if !requestKeyPattern.MatchString(key) || input.Version == 0 || input.Action != "message" && input.Action != "solution" || len(input.Items) < 1 || len(input.Items) > 50 {
		return nil, ErrInvalid
	}
	seen := map[uint64]bool{}
	for _, target := range input.Items {
		if target.ID == 0 || target.Version == 0 || seen[target.ID] {
			return nil, ErrInvalid
		}
		seen[target.ID] = true
	}
	if input.Action == "message" {
		if input.UpdateID == 0 {
			return nil, fmt.Errorf("%w: select an approved public update", ErrInvalid)
		}
		err := s.repo.db.QueryRowContext(ctx, `SELECT body FROM incident_major_updates WHERE tenant_id=? AND incident_id=? AND id=? AND audience='REQUESTERS'`, a.TenantID, parent, input.UpdateID).Scan(&input.Message)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
	}
	maximum := 20000
	if input.Action == "solution" {
		maximum -= len([]rune("Resolution proposal: "))
	}
	if !validText(input.Message, 1, maximum) {
		return nil, ErrInvalid
	}
	digest, err := majorRequestHash(parent, "children", input)
	if err != nil {
		return nil, err
	}
	var batchID uint64
	err = s.majorTx(ctx, a, parent, "incident.major.manage", func(e database.Executor, r Record, a Actor) error {
		existing, err := s.repo.reserveCollaboration(ctx, e, a, key, "major.children", digest)
		if err != nil {
			return err
		}
		if existing > 0 {
			batchID = existing
			return nil
		}
		major, err := s.majorCase(ctx, e, a.TenantID, parent, true)
		if err != nil {
			return err
		}
		if major.Version != input.Version {
			return ErrConflict
		}
		if major.Status != "ACTIVE" {
			return ErrTransition
		}
		result, err := e.ExecContext(ctx, `INSERT INTO incident_major_batches(tenant_id,incident_id,actor_user_id,results) VALUES(?,?,?,JSON_OBJECT())`, a.TenantID, parent, a.UserID)
		if err != nil {
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		batchID = uint64(id)
		return s.repo.completeCollaboration(ctx, e, a, key, batchID)
	})
	if err != nil {
		return nil, err
	}
	results := make([]MajorChildResult, 0, len(input.Items))
	for _, target := range input.Items {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		var raw []byte
		if err = s.repo.db.QueryRowContext(ctx, `SELECT results FROM incident_major_batches WHERE tenant_id=? AND id=? AND actor_user_id=?`, a.TenantID, batchID, a.UserID).Scan(&raw); err != nil {
			return nil, err
		}
		checkpoint := map[string]MajorChildResult{}
		if err = json.Unmarshal(raw, &checkpoint); err != nil {
			return nil, err
		}
		itemKey := fmt.Sprint(target.ID)
		if previous, ok := checkpoint[itemKey]; ok {
			results = append(results, previous)
			continue
		}
		message := input.Message
		if input.Action == "solution" {
			message = "Resolution proposal: " + message
		}
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", key, target.ID)))
		childKey := "major-child-" + hex.EncodeToString(sum[:24])
		err = s.majorChildMessage(ctx, a, parent, target, input.Version, message, childKey)
		result := MajorChildResult{ID: target.ID, Status: 200, Message: "Updated"}
		if err != nil {
			result.Status = errorStatus(err)
			result.Message = "Operation failed"
			if result.Status < 500 {
				result.Message = err.Error()
			}
		}
		encoded, _ := json.Marshal(result)
		if _, err = s.repo.db.ExecContext(ctx, `UPDATE incident_major_batches SET results=JSON_SET(results,?,CAST(? AS JSON)) WHERE tenant_id=? AND id=? AND actor_user_id=?`, "$.\""+itemKey+"\"", encoded, a.TenantID, batchID, a.UserID); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}
