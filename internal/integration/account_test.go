//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/platform/password"
	"github.com/chmajster/OpenServiceNOW/internal/users"
	"github.com/go-chi/chi/v5"
)

func TestOwnAccountGlobalCredentialsAndSessions(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, err := database.OpenMySQL(ctx, cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx := database.NewTxManager(db)
	repo := identity.NewMySQLRepository(db)
	auditRepo := audit.NewMySQLRepository()
	s, err := identity.NewService(repo, tx, auditRepo, cfg.Session, cfg.Auth)
	if err != nil {
		t.Fatal(err)
	}
	b := identity.NewBootstrapper(repo, tx, auditRepo)
	suffix := fmt.Sprint(time.Now().UnixNano())
	old := "temporary-account-password"
	next := strings.Repeat("😀", 12)
	a, err := b.Run(ctx, identity.BootstrapInput{TenantSlug: "account-a-" + suffix, TenantName: "Account A", AdminEmail: "account-a-" + suffix + "@example.test", AdminName: "Original name", AdminPassword: old, RequirePasswordChange: true})
	if err != nil {
		t.Fatal(err)
	}
	bAdmin, err := b.Run(ctx, identity.BootstrapInput{TenantSlug: "account-b-" + suffix, TenantName: "Account B", AdminEmail: "account-b-" + suffix + "@example.test", AdminName: "Other administrator", AdminPassword: "other-account-password"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = db.ExecContext(cleanup, "DELETE FROM audit_events WHERE tenant_id IN (?,?)", a.TenantID, bAdmin.TenantID)
		_, _ = db.ExecContext(cleanup, "DELETE FROM tenants WHERE id IN (?,?)", a.TenantID, bAdmin.TenantID)
		_, _ = db.ExecContext(cleanup, "DELETE FROM users WHERE id IN (?,?)", a.UserID, bAdmin.UserID)
	})
	u := users.NewService(users.NewMySQLRepository(db), tx, auditRepo, identity.NewAdminSafetyRepository(), repo, s)
	if _, err := u.AddExistingMembership(ctx, users.Actor{TenantID: bAdmin.TenantID, UserID: bAdmin.UserID}, users.AddExistingInput{Email: "account-a-" + suffix + "@example.test"}); err != nil {
		t.Fatal(err)
	}
	login := func(tenant, email, pwd string) identity.LoginResult {
		t.Helper()
		result, err := s.Login(ctx, identity.LoginInput{Tenant: tenant, Email: email, Password: pwd}, identity.RequestMeta{})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	email := "account-a-" + suffix + "@example.test"
	current := login("account-a-"+suffix, email, old)
	other := login("account-b-"+suffix, email, old)
	foreign := login("account-b-"+suffix, "account-b-"+suffix+"@example.test", "other-account-password")
	h := identity.NewHandler(s, allowAccountLimiter{}, cfg.Session, cfg.Auth)
	m := identity.NewMiddleware(s, cfg.Session)
	router := chi.NewRouter()
	identity.RegisterRoutes(router, h, m, identity.NewAccountHandler(s, h))
	call := func(method, path string, result identity.LoginResult, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		r.AddCookie(&http.Cookie{Name: cfg.Session.CookieName, Value: result.SessionToken})
		r.AddCookie(&http.Cookie{Name: cfg.Session.CSRFCookieName, Value: result.CSRFToken})
		r.Header.Set("X-CSRF-Token", result.CSRFToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "/api/v1/auth/me", current, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"permissions":[]`) || !strings.Contains(w.Body.String(), `"password_change_required":true`) {
		t.Fatalf("minimal me: %d %s", w.Code, w.Body.String())
	}
	for _, endpoint := range []struct{ method, path string }{{"GET", "/api/v1/auth/sessions"}, {"PUT", "/api/v1/auth/profile"}} {
		if w := call(endpoint.method, endpoint.path, current, ""); w.Code != 403 {
			t.Fatalf("forced account bypass: %d", w.Code)
		}
	}
	input := identity.PasswordInput{CurrentPassword: old, NewPassword: next, CredentialVersion: current.Identity.CredentialVersion}
	wrong := input
	wrong.CurrentPassword = "incorrect-password"
	raw, _ := json.Marshal(wrong)
	if w := call("POST", "/api/v1/auth/password", current, string(raw)); w.Code != 400 {
		t.Fatalf("wrong old password: %d", w.Code)
	}
	// Capture a correctly verified old hash before the concurrent change.
	snapshot, _, err := repo.FindForLogin(ctx, current.Identity.TenantSlug, email)
	if err != nil {
		t.Fatal(err)
	}
	if valid, err := password.Verify(snapshot.PasswordHash, old); err != nil || !valid {
		t.Fatal("old snapshot not verified")
	}
	// Audit failure must roll back credentials, revocation and token rotation together.
	if _, err := s.ChangePassword(ctx, current.Identity, input, identity.RequestMeta{IPAddress: strings.Repeat("x", 100)}); err == nil {
		t.Fatal("audit failure did not fail password transaction")
	}
	if _, err := s.Authenticate(ctx, other.SessionToken); err != nil {
		t.Fatal("failed transaction revoked another session")
	}
	if _, err := s.Authenticate(ctx, current.SessionToken); err != nil {
		t.Fatal("failed transaction rotated current session")
	}
	raw, _ = json.Marshal(input)
	w := call("POST", "/api/v1/auth/password", current, string(raw))
	if w.Code != 200 {
		t.Fatalf("password change: %d %s", w.Code, w.Body.String())
	}
	rotated := current
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == cfg.Session.CookieName {
			rotated.SessionToken = cookie.Value
			if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
				t.Fatal("unsafe session cookie")
			}
		}
		if cookie.Name == cfg.Session.CSRFCookieName {
			rotated.CSRFToken = cookie.Value
		}
	}
	if rotated.SessionToken == current.SessionToken || rotated.CSRFToken == current.CSRFToken {
		t.Fatal("tokens were not renewed")
	}
	if err := s.Logout(ctx, current.Identity, identity.RequestMeta{}); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatalf("stale logout after rotation: %v", err)
	}
	rotated.Identity, err = s.Authenticate(ctx, rotated.SessionToken)
	if err != nil || rotated.Identity.PasswordChangeRequired {
		t.Fatalf("restored rotated session: %v", err)
	}
	for _, token := range []string{current.SessionToken, other.SessionToken} {
		if _, err := s.Authenticate(ctx, token); !errors.Is(err, identity.ErrUnauthenticated) {
			t.Fatalf("old session survived: %v", err)
		}
	}
	if w := call("POST", "/api/v1/auth/password", current, string(raw)); w.Code != 401 {
		t.Fatalf("repeat old request: %d", w.Code)
	}
	if _, err := s.Login(ctx, identity.LoginInput{Tenant: current.Identity.TenantSlug, Email: email, Password: old}, identity.RequestMeta{}); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatal("old password still works")
	}
	err = tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec database.Executor) error {
		_, err := repo.ValidateLoginForUpdate(ctx, exec, current.Identity.TenantID, current.Identity.UserID, snapshot.CredentialVersion)
		return err
	})
	if !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("stale verified credentials can create a session: %v", err)
	}
	other = login("account-b-"+suffix, email, next)
	third := login("account-a-"+suffix, email, next)
	page, err := s.ListOwnSessions(ctx, rotated.Identity, 0, 1)
	if err != nil || len(page.Sessions) != 1 || !page.Sessions[0].Current || page.NextAfterID == 0 {
		t.Fatalf("bounded sessions: %+v %v", page, err)
	}
	page, err = s.ListOwnSessions(ctx, rotated.Identity, page.NextAfterID, 1)
	if err != nil || page.Sessions[0].TenantID != bAdmin.TenantID {
		t.Fatalf("global own sessions: %+v %v", page, err)
	}
	if err := s.RevokeOwnSessions(ctx, foreign.Identity, rotated.Identity.SessionID, identity.RequestMeta{}); !errors.Is(err, identity.ErrNotFound) {
		t.Fatal("organization admin can revoke another global identity")
	}
	if w := call("DELETE", fmt.Sprintf("/api/v1/auth/sessions/%d", rotated.Identity.SessionID), foreign, ""); w.Code != 404 {
		t.Fatalf("foreign session API ownership: %d", w.Code)
	}
	if w := call("PUT", "/api/v1/auth/profile", rotated, fmt.Sprintf(`{"display_name":"Renamed 😀","version":"%d"}`, rotated.Identity.Version)); w.Code != 204 {
		t.Fatalf("profile: %d %s", w.Code, w.Body.String())
	}
	if w := call("PUT", "/api/v1/auth/profile", rotated, fmt.Sprintf(`{"display_name":"Local proposal","version":"%d"}`, rotated.Identity.Version)); w.Code != 409 {
		t.Fatal("profile stale version accepted")
	}
	if err := u.SetMembershipActive(ctx, users.Actor{TenantID: bAdmin.TenantID, UserID: bAdmin.UserID}, a.UserID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, rotated.SessionToken); err != nil {
		t.Fatal("tenant B membership administration revoked tenant A session")
	}
	if _, err := s.Authenticate(ctx, other.SessionToken); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal("disabled membership session survived")
	}
	if err := s.RevokeOwnSessions(ctx, rotated.Identity, third.Identity.SessionID, identity.RequestMeta{}); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeOwnSessions(ctx, rotated.Identity, third.Identity.SessionID, identity.RequestMeta{}); err != nil {
		t.Fatal("session revocation is not idempotent")
	}
	if _, err := s.Authenticate(ctx, third.SessionToken); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal("revoked session survived")
	}
	third = login("account-a-"+suffix, email, next)
	if err := s.RevokeOwnSessions(ctx, rotated.Identity, 0, identity.RequestMeta{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, third.SessionToken); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatal("revoke others did not revoke")
	}
	// A new service and SQL pool must restore the same persisted identity.
	reopened, err := database.OpenMySQL(ctx, cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted, err := identity.NewService(identity.NewMySQLRepository(reopened), database.NewTxManager(reopened), auditRepo, cfg.Session, cfg.Auth)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := restarted.Authenticate(ctx, rotated.SessionToken)
	if err != nil || restored.DisplayName != "Renamed 😀" || restored.CredentialVersion != snapshot.CredentialVersion+1 {
		t.Fatalf("restart persistence: %+v %v", restored, err)
	}
	var events string
	if err := db.QueryRowContext(ctx, `SELECT GROUP_CONCAT(CONCAT(action,COALESCE(CAST(old_values AS CHAR),''),COALESCE(CAST(new_values AS CHAR),''))) FROM audit_events WHERE actor_user_id=? AND action LIKE 'account.%'`, a.UserID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{old, next, rotated.SessionToken, rotated.CSRFToken, snapshot.PasswordHash} {
		if strings.Contains(events, secret) {
			t.Fatal("secret in audit")
		}
	}
	for _, action := range []string{"account.profile.update", "account.password.change", "account.sessions.revoke"} {
		if !strings.Contains(events, action) {
			t.Fatalf("missing audit %s", action)
		}
	}
	// Competing changes with the same authenticated snapshot commit only once.
	results := make(chan struct {
		result identity.LoginResult
		err    error
	}, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			result, err := s.ChangePassword(ctx, restored, identity.PasswordInput{CurrentPassword: next, NewPassword: "concurrent-personal-password", CredentialVersion: restored.CredentialVersion}, identity.RequestMeta{})
			results <- struct {
				result identity.LoginResult
				err    error
			}{result, err}
		}()
	}
	close(start)
	committed := 0
	for range 2 {
		item := <-results
		if item.err == nil {
			committed++
			if _, err := s.Authenticate(ctx, item.result.SessionToken); err != nil {
				t.Fatal(err)
			}
		} else if !errors.Is(item.err, identity.ErrUnauthenticated) && !errors.Is(item.err, identity.ErrAccountConflict) {
			t.Fatalf("competing password change: %v", item.err)
		}
	}
	if committed != 1 {
		t.Fatalf("concurrent changes committed %d times", committed)
	}
}

type allowAccountLimiter struct{}

func (allowAccountLimiter) Allow(context.Context, string, int64, time.Duration) (bool, error) {
	return true, nil
}
