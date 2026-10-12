package config

import (
	"encoding/base64"
	"errors"
	"os"
	"strings"
)

func notificationEncryptionKey() ([]byte, error) {
	value := strings.TrimSpace(os.Getenv("NOTIFICATION_ENCRYPTION_KEY"))
	path := strings.TrimSpace(os.Getenv("NOTIFICATION_ENCRYPTION_KEY_FILE"))
	if value != "" && path != "" {
		return nil, errors.New("configure only one notification encryption key source")
	}
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, errors.New("notification encryption key file unavailable")
		}
		value = strings.TrimSpace(string(data))
	}
	if value == "" {
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(key) != 32 {
		return nil, errors.New("notification encryption key must be base64 encoded 32 bytes")
	}
	return key, nil
}
