package identity

import "strings"

const (
	DefaultAdminLogin    = "admin"
	DefaultAdminEmail    = "admin@example.test"
	DefaultAdminPassword = "admin"
)

// normalizeLoginEmail keeps the administrator alias and its email on the same
// authentication and rate-limit path. Other identities remain email-based.
func normalizeLoginEmail(identifier string) string {
	email := strings.ToLower(strings.TrimSpace(identifier))
	if email == DefaultAdminLogin {
		return DefaultAdminEmail
	}
	return email
}
