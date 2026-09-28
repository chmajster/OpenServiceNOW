package attachments

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestLocalStorageRoundTrip(t *testing.T) {
	store, err := NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	written, err := store.Put(context.Background(), "1/abcdef", strings.NewReader("payload"))
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if written != 7 {
		t.Fatalf("written = %d", written)
	}
	reader, err := store.Open(context.Background(), "1/abcdef")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "payload" {
		t.Fatalf("data = %q", data)
	}
	if err := store.Delete(context.Background(), "1/abcdef"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Open(context.Background(), "1/abcdef"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open() after delete error = %v", err)
	}
}

func TestLocalStorageRejectsTraversal(t *testing.T) {
	store, err := NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"../secret", "/tmp/secret", "a/../../secret", "a\\secret"} {
		if _, err := store.Put(context.Background(), key, strings.NewReader("x")); !errors.Is(err, ErrUnsafeStorageKey) {
			t.Fatalf("key %q error = %v", key, err)
		}
	}
}
