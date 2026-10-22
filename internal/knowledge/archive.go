package knowledge

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

const archiveFormat = "opensnow-knowledge-v1"

var archiveSourcePattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

type archiveManifest struct {
	Format string        `json:"format"`
	Space  archiveSpace  `json:"space"`
	Pages  []archivePage `json:"pages"`
}

type archiveSpace struct {
	Key                string `json:"key"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	ReviewIntervalDays uint16 `json:"review_interval_days"`
}

type archivePage struct {
	SourceID       string     `json:"source_id"`
	ParentSourceID string     `json:"parent_source_id,omitempty"`
	Title          string     `json:"title"`
	Status         string     `json:"status"`
	ReviewDueAt    *time.Time `json:"review_due_at,omitempty"`
	File           string     `json:"file"`
	Body           string     `json:"-"`
	UpdatedAt      time.Time  `json:"-"`
}

func (s *Service) Export(ctx context.Context, actor Actor, spaceID uint64) ([]byte, string, error) {
	if spaceID == 0 {
		return nil, "", ErrInvalid
	}
	var output []byte
	var filename string
	err := s.write(ctx, actor, requireManage, func(exec database.Executor, current Actor) error {
		space, err := getSpaceBase(ctx, exec, current.TenantID, spaceID, true)
		if err != nil {
			return err
		}
		rows, err := exec.QueryContext(ctx, `
			SELECT id,parent_id,title,body,status,review_due_at,updated_at
			FROM knowledge_pages WHERE tenant_id=? AND space_id=? ORDER BY id`, current.TenantID, spaceID)
		if err != nil {
			return err
		}
		defer rows.Close()
		manifest := archiveManifest{
			Format: archiveFormat,
			Space:  archiveSpace{Key: space.Key, Name: space.Name, Description: space.Description, ReviewIntervalDays: space.ReviewIntervalDays},
			Pages:  []archivePage{},
		}
		total := 0
		for rows.Next() {
			var id uint64
			var parent sql.Null[uint64]
			var review sql.NullTime
			var item archivePage
			if err := rows.Scan(&id, &parent, &item.Title, &item.Body, &item.Status, &review, &item.UpdatedAt); err != nil {
				return err
			}
			item.SourceID = fmt.Sprint(id)
			if parent.Valid {
				item.ParentSourceID = fmt.Sprint(parent.V)
			}
			if review.Valid {
				item.ReviewDueAt = &review.Time
			}
			item.File = "pages/" + item.SourceID + ".md"
			total += len(item.Body)
			if total > MaxArchiveBytes || len(manifest.Pages) >= MaxImportPages {
				return ErrTooLarge
			}
			manifest.Pages = append(manifest.Pages, item)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		encoded, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return err
		}
		var buffer bytes.Buffer
		writer := zip.NewWriter(&buffer)
		if err := writeArchiveFile(writer, "manifest.json", encoded, space.UpdatedAt); err != nil {
			_ = writer.Close()
			return err
		}
		for _, item := range manifest.Pages {
			if err := writeArchiveFile(writer, item.File, []byte(item.Body), item.UpdatedAt); err != nil {
				_ = writer.Close()
				return err
			}
		}
		if err := writer.Close(); err != nil {
			return err
		}
		if buffer.Len() > MaxArchiveBytes {
			return ErrTooLarge
		}
		output = buffer.Bytes()
		filename = "opensnow-" + strings.ToLower(space.Key) + "-documentation.zip"
		digest := sha256.Sum256(output)
		return s.appendAudit(ctx, exec, current, "knowledge.space.exported", "knowledge_space", spaceID, nil, map[string]any{
			"pages": len(manifest.Pages), "archive_bytes": len(output), "archive_sha256": fmt.Sprintf("%x", digest[:]),
		})
	})
	return output, filename, err
}

func writeArchiveFile(writer *zip.Writer, name string, content []byte, changed time.Time) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(0o600)
	header.Modified = changed.UTC()
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = entry.Write(content)
	return err
}

func (s *Service) Import(ctx context.Context, actor Actor, spaceID uint64, data []byte, key string) (ImportResult, error) {
	result := ImportResult{SpaceID: spaceID}
	if spaceID == 0 || len(data) == 0 {
		return result, ErrInvalid
	}
	if int64(len(data)) > MaxImportBytes {
		return result, ErrTooLarge
	}
	manifest, err := readArchive(data)
	if err != nil {
		return result, err
	}
	digest := sha256.Sum256(append([]byte(fmt.Sprintf("%d:", spaceID)), data...))
	err = s.write(ctx, actor, requireManage, func(exec database.Executor, current Actor) error {
		existing, err := workrecords.ReserveOperation(ctx, exec, current.Actor, key, "knowledge_import", digest)
		if err != nil {
			if errors.Is(err, workrecords.ErrInvalid) {
				return ErrInvalid
			}
			return err
		}
		if existing > 0 {
			if existing != spaceID {
				return ErrConflict
			}
			result.Replayed = true
			return nil
		}
		space, err := getSpaceBase(ctx, exec, current.TenantID, spaceID, true)
		if err != nil {
			return err
		}
		if space.Status != SpaceActive {
			return ErrConflict
		}
		ids := map[string]uint64{}
		pending := append([]archivePage(nil), manifest.Pages...)
		now := time.Now().UTC()
		for len(pending) > 0 {
			progress := false
			next := make([]archivePage, 0, len(pending))
			for _, item := range pending {
				var parent *uint64
				if item.ParentSourceID != "" {
					value, ok := ids[item.ParentSourceID]
					if !ok {
						next = append(next, item)
						continue
					}
					parent = &value
				}
				input := PageInput{ParentID: parent, Title: item.Title, Body: item.Body, Status: item.Status, ReviewDueAt: item.ReviewDueAt}
				applyReviewDefault(&input, space.ReviewIntervalDays)
				created, err := exec.ExecContext(ctx, `
					INSERT INTO knowledge_pages(
						tenant_id,space_id,parent_id,title,body,status,review_due_at,created_by,
						updated_by,created_at,updated_at
					) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
					current.TenantID, spaceID, input.ParentID, input.Title, input.Body, input.Status,
					input.ReviewDueAt, current.UserID, current.UserID, now, now)
				if err != nil {
					return translate(err)
				}
				inserted, err := created.LastInsertId()
				if err != nil {
					return err
				}
				id := uint64(inserted)
				ids[item.SourceID] = id
				page := Page{ID: id, SpaceID: spaceID, ParentID: parent, Title: input.Title, Body: input.Body, Status: input.Status, ReviewDueAt: input.ReviewDueAt, Version: 1, CreatedBy: current.UserID, UpdatedBy: current.UserID, CreatedAt: now, UpdatedAt: now}
				if err := insertVersion(ctx, exec, current.TenantID, page, current.UserID, now); err != nil {
					return err
				}
				progress = true
				result.Created++
			}
			if !progress {
				return ErrInvalid
			}
			pending = next
		}
		if err := workrecords.CompleteOperation(ctx, exec, current.Actor, key, spaceID); err != nil {
			return err
		}
		return s.appendAudit(ctx, exec, current, "knowledge.space.imported", "knowledge_space", spaceID, nil, map[string]any{
			"pages_created": result.Created, "archive_bytes": len(data), "archive_sha256": fmt.Sprintf("%x", digest[:]),
			"source_space_key": manifest.Space.Key,
		})
	})
	return result, err
}

func readArchive(data []byte) (archiveManifest, error) {
	empty := archiveManifest{Pages: []archivePage{}}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(reader.File) == 0 || len(reader.File) > MaxImportPages+1 {
		return empty, ErrInvalid
	}
	files := map[string][]byte{}
	total := 0
	for _, item := range reader.File {
		name := item.Name
		if item.FileInfo().IsDir() || path.Clean(name) != name || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
			return empty, ErrInvalid
		}
		if _, exists := files[name]; exists || item.UncompressedSize64 > MaxArchiveBytes {
			return empty, ErrInvalid
		}
		stream, err := item.Open()
		if err != nil {
			return empty, ErrInvalid
		}
		content, readErr := io.ReadAll(io.LimitReader(stream, int64(MaxArchiveBytes-total+1)))
		closeErr := stream.Close()
		if readErr != nil || closeErr != nil {
			return empty, ErrInvalid
		}
		total += len(content)
		if total > MaxArchiveBytes {
			return empty, ErrTooLarge
		}
		files[name] = content
	}
	manifestData, ok := files["manifest.json"]
	if !ok {
		return empty, ErrInvalid
	}
	delete(files, "manifest.json")
	decoder := json.NewDecoder(bytes.NewReader(manifestData))
	decoder.DisallowUnknownFields()
	var manifest archiveManifest
	if err := decoder.Decode(&manifest); err != nil {
		return empty, ErrInvalid
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return empty, ErrInvalid
	}
	if manifest.Format != archiveFormat || len(manifest.Pages) == 0 || len(manifest.Pages) > MaxImportPages ||
		!text(manifest.Space.Key, 1, 16) || !text(manifest.Space.Name, 1, 160) ||
		!text(manifest.Space.Description, 0, 10000) || manifest.Space.ReviewIntervalDays > 3650 {
		return empty, ErrInvalid
	}
	sources := make(map[string]bool, len(manifest.Pages))
	for index := range manifest.Pages {
		item := &manifest.Pages[index]
		if !archiveSourcePattern.MatchString(item.SourceID) || sources[item.SourceID] || item.ParentSourceID == item.SourceID ||
			item.File != "pages/"+item.SourceID+".md" {
			return empty, ErrInvalid
		}
		sources[item.SourceID] = true
		body, exists := files[item.File]
		if !exists {
			return empty, ErrInvalid
		}
		delete(files, item.File)
		input, err := normalizePage(PageInput{Title: item.Title, Body: string(body), Status: item.Status, ReviewDueAt: item.ReviewDueAt}, true)
		if err != nil {
			return empty, err
		}
		item.Title, item.Body, item.Status, item.ReviewDueAt = input.Title, input.Body, input.Status, input.ReviewDueAt
	}
	if len(files) != 0 {
		return empty, ErrInvalid
	}
	for _, item := range manifest.Pages {
		if item.ParentSourceID != "" && !sources[item.ParentSourceID] {
			return empty, ErrInvalid
		}
	}
	if archiveHasCycle(manifest.Pages) {
		return empty, ErrInvalid
	}
	sort.SliceStable(manifest.Pages, func(i, j int) bool { return manifest.Pages[i].SourceID < manifest.Pages[j].SourceID })
	return manifest, nil
}

func archiveHasCycle(pages []archivePage) bool {
	parents := make(map[string]string, len(pages))
	for _, page := range pages {
		parents[page.SourceID] = page.ParentSourceID
	}
	for source := range parents {
		seen := map[string]bool{}
		for current := source; current != ""; current = parents[current] {
			if seen[current] {
				return true
			}
			seen[current] = true
		}
	}
	return false
}
