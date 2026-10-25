package cmdb

import (
	"context"
	"encoding/json"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

func csvSourceScope(source DataSource) DataSource {
	if source.Key == "csv-import" && source.ScopeKey == "" {
		source.ScopeKey = "default"
	}
	return source
}

func (s *Service) csvImportSource(ctx context.Context, e database.Executor, a Actor, sourceID uint64) (DataSource, error) {
	if sourceID == 0 {
		source, err := s.ensureSystemSource(ctx, e, a, "csv-import", "CSV import", "CSV")
		if err != nil {
			return DataSource{}, err
		}
		return csvSourceScope(source), nil
	}
	if !canManageIRE(a) {
		return DataSource{}, workrecords.ErrForbidden
	}
	source, err := s.sourceInTx(ctx, e, a.TenantID, sourceID, true)
	if err != nil {
		return DataSource{}, err
	}
	if source.Type != "CSV" || source.State != "ACTIVE" {
		return DataSource{}, ErrConflict
	}
	return csvSourceScope(source), nil
}

func (s *Service) applyCSVCIObservation(ctx context.Context, e database.Executor, a Actor, i Import, row ImportRow, record Record, source DataSource) (uint64, error) {
	var err error
	observation := Observation{
		NativeKey:  row.Identity,
		ScopeKey:   source.ScopeKey,
		ObservedAt: i.UpdatedAt,
		Values:     observationValues(record.Fields),
	}
	if observation.ObservedAt.IsZero() {
		observation.ObservedAt = i.CreatedAt
	}
	result, err := s.applyObservationInTx(ctx, e, a, source, observation, "CSV_IMPORT", i.ID, row.RuleVersion)
	if err != nil {
		return 0, err
	}
	if result.State != "APPLIED" || row.RecordID > 0 && result.CIID != row.RecordID || row.RecordID == 0 && row.Action == "CREATE" && len(result.Candidates) > 0 {
		return 0, ErrConflict
	}
	if i.CustomFieldsVersion > 0 && s.Metadata != nil {
		configured := map[string]bool{}
		for key := range i.Mapping {
			if len(key) > len("custom_fields.") && key[:len("custom_fields.")] == "custom_fields." {
				configured[key] = true
			}
		}
		if len(configured) > 0 {
			current, err := s.GetInTx(ctx, e, a, result.CIID, true)
			if err != nil {
				return 0, err
			}
			merged := map[string]json.RawMessage{}
			for key, raw := range current.CustomFields {
				merged[key] = append(json.RawMessage(nil), raw...)
			}
			type customChange struct {
				field string
				value json.RawMessage
				clear bool
			}
			changes := []customChange{}
			for target := range configured {
				field := target[len("custom_fields."):]
				value, hasValue := record.CustomFields[field]
				previous, hadValue := current.CustomFields[field]
				if hasValue == hadValue && (!hasValue || valuesEqual(previous, value)) {
					continue
				}
				accepted, _, checkErr := s.fieldAcceptable(ctx, e, a.TenantID, current.ID, current.Class, target, previous, source, observation.ObservedAt)
				if checkErr != nil {
					return 0, checkErr
				}
				if !accepted {
					return 0, ErrConflict
				}
				if hasValue {
					merged[field] = append(json.RawMessage(nil), value...)
				} else {
					delete(merged, field)
				}
				changes = append(changes, customChange{field: target, value: append(json.RawMessage(nil), value...), clear: !hasValue})
			}
			if len(changes) == 0 {
				return result.CIID, nil
			}
			current.CustomFields = merged
			current.CustomFieldsVersion = i.CustomFieldsVersion
			saved, err := s.SaveInTxAtMetadataVersion(ctx, e, a, current.ID, current, i.CustomFieldsVersion)
			if err != nil {
				return 0, err
			}
			for _, change := range changes {
				value := change.value
				if change.clear {
					value = json.RawMessage(`null`)
				}
				if err = s.writeProvenance(ctx, e, a.TenantID, saved.ID, source.ID, change.field, "CSV_IMPORT", i.ID, result.RuleVersion, source.Priority, observation.ObservedAt, value); err != nil {
					return 0, err
				}
			}
		}
	}
	return result.CIID, nil
}
