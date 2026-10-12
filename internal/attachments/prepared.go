package attachments

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"unicode"
	"unicode/utf8"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

const MaxFileBytes int64 = 10 << 20
const MaxMultipartBytes int64 = 11 << 20

// Prepared files are private until metadata commits. Unreferenced files left by
// crashes/uncertain commits are reconciled after a grace period, never blindly
// removed after COMMIT's acknowledgement is lost.
type Prepared struct {
	Key, ContentType string
	Size             uint64
	SHA256           [32]byte
}

func (s *Service) Prepare(ctx context.Context, input CreateInput, content io.Reader) (Prepared, error) {
	if err := validateCreateInput(input); err != nil || content == nil {
		return Prepared{}, ErrInvalid
	}
	for _, r := range input.FileName {
		if unicode.IsControl(r) {
			return Prepared{}, ErrInvalid
		}
	}
	if !utf8.ValidString(input.FileName) {
		return Prepared{}, ErrInvalid
	}
	reader := bufio.NewReader(content)
	sniff, err := reader.Peek(512)
	if err != nil && err != io.EOF && err != bufio.ErrBufferFull {
		return Prepared{}, err
	}
	key, err := newStorageKey(input.TenantID)
	if err != nil {
		return Prepared{}, err
	}
	hash := sha256.New()
	size, err := s.storage.Put(ctx, key, io.TeeReader(io.LimitReader(reader, s.maxBytes+1), hash))
	if err != nil || size > s.maxBytes {
		_ = s.storage.Delete(context.WithoutCancel(ctx), key)
		if err != nil {
			return Prepared{}, err
		}
		return Prepared{}, ErrTooLarge
	}
	if size == 0 {
		_ = s.storage.Delete(context.WithoutCancel(ctx), key)
		return Prepared{}, fmt.Errorf("%w: pusty plik", ErrInvalid)
	}
	var sum [32]byte
	copy(sum[:], hash.Sum(nil))
	return Prepared{key, http.DetectContentType(sniff), uint64(size), sum}, nil
}
func (s *Service) CreatePreparedInTx(ctx context.Context, exec database.Executor, input CreateInput, p Prepared) (Attachment, error) {
	if s.transactional == nil || p.Key == "" {
		return Attachment{}, ErrInvalid
	}
	return s.transactional.CreateTx(ctx, exec, input, p.Key, p.ContentType, p.Size, p.SHA256)
}
func (s *Service) Discard(ctx context.Context, p Prepared) error { return s.storage.Delete(ctx, p.Key) }
