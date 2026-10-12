package attachments

import (
	"bytes"
	"context"
	"io"
	"testing"
)

func TestPreparedSniffsInitialBytesBeforeStreamingReusesBuffer(t *testing.T) {
	storage, err := NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{storage: storage, maxBytes: MaxFileBytes}
	// A PNG header followed by enough text to overwrite a buffered-reader peek.
	content := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte("text"), 32<<10)...)
	prepared, err := service.Prepare(context.Background(), CreateInput{TenantID: 1, ObjectType: "incident", ObjectID: "1", FileName: "obraz.png"}, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if prepared.ContentType != "image/png" {
		t.Fatal("MIME was inferred from the wrong buffer", prepared.ContentType)
	}
	reader, err := storage.Open(context.Background(), prepared.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	stored, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(stored, content) {
		t.Fatal("stored bytes changed", err)
	}
}
