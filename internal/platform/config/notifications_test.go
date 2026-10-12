package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotificationKeySources(t *testing.T) {
	t.Setenv("NOTIFICATION_ENCRYPTION_KEY", "")
	t.Setenv("NOTIFICATION_ENCRYPTION_KEY_FILE", "")
	key, err := notificationEncryptionKey()
	if err != nil || len(key) != 0 {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))
	t.Setenv("NOTIFICATION_ENCRYPTION_KEY", encoded)
	key, err = notificationEncryptionKey()
	if err != nil || len(key) != 32 {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "key")
	if err = os.WriteFile(file, []byte(encoded+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOTIFICATION_ENCRYPTION_KEY_FILE", file)
	if _, err = notificationEncryptionKey(); err == nil {
		t.Fatal("ambiguous key sources accepted")
	}
	t.Setenv("NOTIFICATION_ENCRYPTION_KEY", "")
	if _, err = notificationEncryptionKey(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOTIFICATION_ENCRYPTION_KEY_FILE", "")
	t.Setenv("NOTIFICATION_ENCRYPTION_KEY", "sensitive-invalid-secret")
	if _, err = notificationEncryptionKey(); err == nil || strings.Contains(err.Error(), "sensitive-invalid-secret") {
		t.Fatal("invalid key leaked or accepted")
	}
}
