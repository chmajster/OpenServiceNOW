package cmdb

import (
	"context"
	"crypto/sha256"
	"encoding/json"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

func (s *Service) SaveManual(ctx context.Context, a Actor, id uint64, in Record, key, reason string) (Record, error) {
	var out Record
	if len(reason) > 500 {
		return out, ErrInvalid
	}
	if reason == "" {
		reason = "Edycja ręczna w CMDB"
	}
	err := s.ActorTx(ctx, a, func(e database.Executor, current Actor) error {
		if !Writable(current) {
			return workrecords.ErrForbidden
		}
		if id == 0 {
			fields, err := s.normalize(ctx, e, current, in.Fields)
			if err != nil {
				return err
			}
			in.Fields = fields
			raw, err := json.Marshal(in.Fields)
			if err != nil {
				return err
			}
			existing, err := workrecords.ReserveOperation(ctx, e, current.Actor, key, "ci_create", sha256.Sum256(raw))
			if err != nil {
				return err
			}
			if existing > 0 {
				out, err = s.GetInTx(ctx, e, current, existing, false)
				return err
			}
		}
		var before *Record
		if id > 0 {
			previous, err := s.GetInTx(ctx, e, current, id, true)
			if err != nil {
				return err
			}
			before = &previous
		}
		var err error
		out, err = s.SaveInTx(ctx, e, current, id, in)
		if err != nil {
			return err
		}
		if err = s.RecordManualChanges(ctx, e, current, before, out, reason); err != nil {
			return err
		}
		if id == 0 {
			return workrecords.CompleteOperation(ctx, e, current.Actor, key, out.ID)
		}
		return nil
	})
	return out, err
}
