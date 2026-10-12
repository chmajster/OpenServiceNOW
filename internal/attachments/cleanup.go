package attachments

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/jobs"
)

const CleanupJobType = "system.attachment_cleanup"

var managedFile = regexp.MustCompile(`^[0-9]+/(?:[a-f0-9]{32}|\.upload-[A-Za-z0-9]+)$`)

// Sweep only managed regular files older than the grace period. References are
// checked in bounded SQL batches; rollback/crash orphans and deleted bytes can
// be removed without racing in-flight uploads or ambiguous COMMIT outcomes.
func (s *LocalStorage) Cleanup(ctx context.Context, repo *Repository, before time.Time) (int, error) {
	if repo == nil || before.IsZero() || before.After(time.Now().UTC().Add(-time.Hour)) {
		return 0, ErrInvalid
	}
	keys := []string{}
	removed := 0
	flush := func() error {
		if len(keys) == 0 {
			return nil
		}
		args := make([]any, len(keys))
		for i, k := range keys {
			args[i] = k
		}
		query := `SELECT storage_key FROM attachments WHERE status<>'DELETED' AND storage_key IN (` + strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",") + `)`
		rows, err := repo.db.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		live := map[string]bool{}
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				rows.Close()
				return err
			}
			live[k] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, key := range keys {
			if !live[key] {
				if err := s.Delete(ctx, key); err != nil {
					return err
				}
				removed++
			}
		}
		keys = keys[:0]
		return nil
	}
	err := filepath.WalkDir(s.root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		key, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		key = filepath.ToSlash(key)
		if !managedFile.MatchString(key) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.ModTime().Before(before) {
			return nil
		}
		keys = append(keys, key)
		if len(keys) == 500 {
			return flush()
		}
		return nil
	})
	if err != nil {
		return removed, err
	}
	return removed, flush()
}
func EnqueueCleanup(ctx context.Context, repo *jobs.Repository, now time.Time) (jobs.Job, bool, error) {
	payload, _ := json.Marshal(struct {
		Before time.Time `json:"before"`
	}{now.UTC().Add(-24 * time.Hour)})
	return repo.Enqueue(ctx, jobs.EnqueueInput{Type: CleanupJobType, Payload: payload, MaxAttempts: 3, AvailableAt: now.UTC(), IdempotencyKey: "attachment-cleanup:" + now.UTC().Truncate(time.Hour).Format("20060102T1504Z")})
}
func CleanupHandler(storage *LocalStorage, repo *Repository) jobs.Handler {
	return func(ctx context.Context, j jobs.Job) (json.RawMessage, error) {
		if j.Type != CleanupJobType || j.TenantID != 0 || storage == nil || repo == nil {
			return nil, jobs.ErrInvalid
		}
		var p struct {
			Before time.Time `json:"before"`
		}
		if err := json.Unmarshal(j.Payload, &p); err != nil {
			return nil, err
		}
		deleted, err := storage.Cleanup(ctx, repo, p.Before)
		if err != nil {
			return nil, fmt.Errorf("attachment cleanup: %w", err)
		}
		return json.Marshal(map[string]int{"removed_files": deleted})
	}
}
