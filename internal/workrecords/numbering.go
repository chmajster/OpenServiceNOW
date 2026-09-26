package workrecords

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

var (
	ErrInvalidSequenceConfiguration = errors.New("invalid record sequence configuration")
	ErrSequenceConfigurationConflict = errors.New("record sequence configuration conflict")
	recordTypePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)
	prefixPattern = regexp.MustCompile(`^[A-Z0-9]{1,16}$`)
)

type Allocator struct {
	tx *database.TxManager
}

func NewAllocator(tx *database.TxManager) *Allocator {
	return &Allocator{tx: tx}
}

func (a *Allocator) Next(
	ctx context.Context,
	tenantID uint64,
	recordType string,
	prefix string,
	width int,
) (string, error) {
	recordType = strings.TrimSpace(recordType)
	prefix = strings.TrimSpace(prefix)
	if err := validateSequenceInput(tenantID, recordType, prefix, width); err != nil {
		return "", err
	}

	var number string
	err := a.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec database.Executor) error {
		if _, err := exec.ExecContext(ctx, `
			INSERT IGNORE INTO record_sequences (tenant_id, record_type, prefix, width, next_value)
			VALUES (?, ?, ?, ?, 1)
		`, tenantID, recordType, prefix, width); err != nil {
			return fmt.Errorf("ensure record sequence: %w", err)
		}

		var storedPrefix string
		var storedWidth int
		var nextValue uint64
		if err := exec.QueryRowContext(ctx, `
			SELECT prefix, width, next_value
			FROM record_sequences
			WHERE tenant_id = ? AND record_type = ?
			FOR UPDATE
		`, tenantID, recordType).Scan(&storedPrefix, &storedWidth, &nextValue); err != nil {
			return fmt.Errorf("lock record sequence: %w", err)
		}
		if storedPrefix != prefix || storedWidth != width {
			return ErrSequenceConfigurationConflict
		}
		if nextValue == math.MaxUint64 {
			return fmt.Errorf("record sequence exhausted")
		}
		if _, err := exec.ExecContext(ctx, `
			UPDATE record_sequences
			SET next_value = ?
			WHERE tenant_id = ? AND record_type = ?
		`, nextValue+1, tenantID, recordType); err != nil {
			return fmt.Errorf("advance record sequence: %w", err)
		}
		number = formatNumber(prefix, width, nextValue)
		return nil
	})
	if err != nil {
		return "", err
	}
	return number, nil
}

func validateSequenceInput(tenantID uint64, recordType, prefix string, width int) error {
	if tenantID == 0 ||
		!recordTypePattern.MatchString(recordType) ||
		!prefixPattern.MatchString(prefix) ||
		width < 1 || width > 20 {
		return ErrInvalidSequenceConfiguration
	}
	return nil
}

func formatNumber(prefix string, width int, value uint64) string {
	return fmt.Sprintf("%s%0*d", prefix, width, value)
}
