package notifications

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/smtp"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/incidents"
	"github.com/chmajster/OpenServiceNOW/internal/jobs"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

func (s *Service) seal(tenant uint64, secret string) ([]byte, error) {
	if len(s.key) != 32 {
		return nil, ErrEncryption
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, ErrEncryption
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrEncryption
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, ErrEncryption
	}
	return gcm.Seal(nonce, nonce, []byte(secret), []byte(fmt.Sprintf("smtp:v1:%d", tenant))), nil
}
func (s *Service) unseal(tenant uint64, data []byte) (string, error) {
	if len(data) == 0 {
		return "", nil
	}
	if len(s.key) != 32 {
		return "", ErrEncryption
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", ErrEncryption
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(data) < gcm.NonceSize() {
		return "", ErrEncryption
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], []byte(fmt.Sprintf("smtp:v1:%d", tenant)))
	if err != nil {
		return "", ErrEncryption
	}
	return string(plain), nil
}
func validAddress(value string) bool {
	if strings.ContainsAny(value, "\r\n") || len(value) > 254 {
		return false
	}
	a, err := mail.ParseAddress(value)
	return err == nil && a.Address == value && strings.Contains(value, "@")
}

var providerPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

func (s *Service) Config(ctx context.Context, tenant uint64) (SMTPConfig, error) {
	return s.config(ctx, s.db, tenant)
}
func (s *Service) config(ctx context.Context, e database.Executor, tenant uint64) (SMTPConfig, error) {
	c := SMTPConfig{Port: 587, TLSMode: "STARTTLS", EncryptionReady: len(s.key) == 32, InsecureAllowed: s.allowInsecure}
	err := e.QueryRowContext(ctx, `SELECT provider_id,enabled,host,port,tls_mode,username,password_cipher,from_address,version FROM notification_smtp_config WHERE tenant_id=?`, tenant).Scan(&c.ProviderID, &c.Enabled, &c.Host, &c.Port, &c.TLSMode, &c.Username, &c.cipher, &c.From, &c.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	c.HasPassword = len(c.cipher) > 0
	return c, err
}
func (s *Service) SaveConfig(ctx context.Context, tenant, user uint64, in ConfigInput) error {
	in.Host = strings.TrimSpace(in.Host)
	in.From = strings.TrimSpace(in.From)
	in.ProviderID = strings.TrimSpace(in.ProviderID)
	if !providerPattern.MatchString(in.ProviderID) || in.Host == "" || len(in.Host) > 253 || strings.ContainsAny(in.Host, " /\\\r\n\t") || in.Port < 1 || in.Port > 65535 || !validAddress(in.From) || len(in.Username) > 254 || strings.ContainsAny(in.Username, "\r\n") || in.TLSMode != "STARTTLS" && in.TLSMode != "TLS" && in.TLSMode != "NONE" || in.TLSMode == "NONE" && (!s.allowInsecure || in.Username != "") || in.Password != nil && len(*in.Password) > 1024 {
		return ErrInvalid
	}
	return s.tx.WithTx(ctx, nil, func(e database.Executor) error {
		var tenantStatus string
		if err := e.QueryRowContext(ctx, `SELECT status FROM tenants WHERE id=? FOR UPDATE`, tenant).Scan(&tenantStatus); err != nil {
			return err
		}
		old, err := s.config(ctx, e, tenant)
		if err != nil {
			return err
		}
		if old.Version != in.Version {
			return ErrConflict
		}
		encrypted := old.cipher
		if in.Password != nil {
			encrypted = nil
			if *in.Password != "" {
				encrypted, err = s.seal(tenant, *in.Password)
				if err != nil {
					return err
				}
			}
		}
		if in.Username != "" && len(encrypted) == 0 {
			return ErrInvalid
		}
		if old.Version == 0 {
			_, err = e.ExecContext(ctx, `INSERT INTO notification_smtp_config(tenant_id,provider_id,enabled,host,port,tls_mode,username,password_cipher,from_address) VALUES(?,?,?,?,?,?,?,?,?)`, tenant, in.ProviderID, in.Enabled, in.Host, in.Port, in.TLSMode, in.Username, encrypted, in.From)
		} else {
			_, err = e.ExecContext(ctx, `UPDATE notification_smtp_config SET provider_id=?,enabled=?,host=?,port=?,tls_mode=?,username=?,password_cipher=?,from_address=?,version=version+1 WHERE tenant_id=?`, in.ProviderID, in.Enabled, in.Host, in.Port, in.TLSMode, in.Username, encrypted, in.From, tenant)
		}
		if err != nil {
			return err
		}
		return auditChange(ctx, e, tenant, user, "notification.smtp.configured", tenant)
	})
}
func (s *Service) createMail(ctx context.Context, e database.Executor, tenant, user, notification uint64) (uint64, error) {
	c, err := s.config(ctx, e, tenant)
	if err != nil {
		return 0, err
	}
	status := "QUEUED"
	code := ""
	if !c.Enabled || c.Version == 0 {
		status = "NOT_CONFIGURED"
		code = "SMTP_NOT_CONFIGURED"
	}
	var n any
	if notification > 0 {
		n = notification
	}
	result, err := e.ExecContext(ctx, `INSERT INTO notification_mail_deliveries(tenant_id,user_id,notification_id,provider_id,status,error_code,finished_at) VALUES(?,?,?,?,?,?,IF(?='NOT_CONFIGURED',UTC_TIMESTAMP(6),NULL))`, tenant, user, n, c.ProviderID, status, code, status)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if status == "QUEUED" {
		err = s.enqueueMail(ctx, e, tenant, uint64(id), 1)
	}
	return uint64(id), err
}
func (s *Service) enqueueMail(ctx context.Context, e database.Executor, tenant, id, version uint64) error {
	payload, _ := json.Marshal(struct {
		ID uint64 `json:"delivery_id,string"`
	}{id})
	job, _, err := jobs.NewRepository(s.db).EnqueueInTx(ctx, e, jobs.EnqueueInput{TenantID: tenant, Type: SMTPJobType, Payload: payload, MaxAttempts: 3, IdempotencyKey: fmt.Sprintf("smtp:%d:%d", id, version)})
	if err != nil {
		return err
	}
	_, err = e.ExecContext(ctx, `UPDATE notification_mail_deliveries SET job_id=?,status='QUEUED',error_code='',finished_at=NULL WHERE tenant_id=? AND id=?`, job.ID, tenant, id)
	return err
}

// Test deliveries always target the authenticated administrator's own verified
// login address. A repeated request key reuses the job and cannot send again.
func (s *Service) TestSMTP(ctx context.Context, tenant, user uint64, key string) (uint64, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`).MatchString(key) {
		return 0, ErrInvalid
	}
	var id uint64
	err := s.tx.WithTx(ctx, nil, func(e database.Executor) error {
		var tenantState string
		if err := e.QueryRowContext(ctx, `SELECT status FROM tenants WHERE id=? FOR UPDATE`, tenant).Scan(&tenantState); err != nil {
			return err
		}
		var found uint64
		err := e.QueryRowContext(ctx, `SELECT d.id FROM notification_mail_deliveries d JOIN jobs j ON j.id=d.job_id WHERE d.tenant_id=? AND d.user_id=? AND j.idempotency_key=?`, tenant, user, "smtp-test:"+fmt.Sprint(user)+":"+key).Scan(&found)
		if err == nil {
			id = found
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		c, err := s.config(ctx, e, tenant)
		if err != nil {
			return err
		}
		if !c.Enabled {
			return ErrInvalid
		}
		result, err := e.ExecContext(ctx, `INSERT INTO notification_mail_deliveries(tenant_id,user_id,provider_id) VALUES(?,?,?)`, tenant, user, c.ProviderID)
		if err != nil {
			return err
		}
		value, err := result.LastInsertId()
		if err != nil {
			return err
		}
		id = uint64(value)
		payload, _ := json.Marshal(struct {
			ID uint64 `json:"delivery_id,string"`
		}{id})
		job, created, err := jobs.NewRepository(s.db).EnqueueInTx(ctx, e, jobs.EnqueueInput{TenantID: tenant, Type: SMTPJobType, Payload: payload, MaxAttempts: 1, IdempotencyKey: "smtp-test:" + fmt.Sprint(user) + ":" + key})
		if err != nil {
			return err
		}
		if !created {
			return ErrConflict
		}
		_, err = e.ExecContext(ctx, `UPDATE notification_mail_deliveries SET job_id=? WHERE id=?`, job.ID, id)
		if err != nil {
			return err
		}
		return auditChange(ctx, e, tenant, user, "notification.smtp.test", id)
	})
	return id, err
}
func (s *Service) MailHandler(ctx context.Context, job jobs.Job) (json.RawMessage, error) {
	var input struct {
		ID uint64 `json:"delivery_id,string"`
	}
	if job.TenantID == 0 || job.Type != SMTPJobType || json.Unmarshal(job.Payload, &input) != nil || input.ID == 0 {
		return nil, jobs.ErrInvalid
	}
	var c SMTPConfig
	var to, summary string
	var incident uint64
	var sequence uint32
	skip := ""
	already := false
	err := s.tx.WithTx(ctx, nil, func(e database.Executor) error {
		var jobOwner string
		if err := e.QueryRowContext(ctx, `SELECT lease_owner FROM jobs WHERE id=? AND tenant_id=? AND status='RUNNING' AND lease_owner=? AND lease_expires_at>UTC_TIMESTAMP(6) FOR UPDATE`, job.ID, job.TenantID, job.LeaseOwner).Scan(&jobOwner); err != nil {
			return jobs.ErrLeaseLost
		}
		var d MailDelivery
		var notification sql.Null[uint64]
		var storedJob uint64
		if err := e.QueryRowContext(ctx, `SELECT user_id,notification_id,status,attempt_count,job_id FROM notification_mail_deliveries WHERE tenant_id=? AND id=? FOR UPDATE`, job.TenantID, input.ID).Scan(&d.UserID, &notification, &d.Status, &d.Attempts, &storedJob); err != nil {
			return err
		}
		if storedJob != job.ID {
			return jobs.ErrInvalid
		}
		if d.Status == "DELIVERED" || d.Status == "SKIPPED" {
			already = true
			return nil
		}
		a, err := s.currentActor(ctx, e, job.TenantID, d.UserID)
		if err != nil {
			return err
		}
		if notification.Valid {
			var kind, visibility string
			if err = e.QueryRowContext(ctx, `SELECT incident_id,summary,kind,visibility FROM notifications WHERE tenant_id=? AND user_id=? AND id=?`, job.TenantID, d.UserID, notification.V).Scan(&incident, &summary, &kind, &visibility); err != nil {
				return err
			}
			r, err := incidents.NewRepository(s.db).Get(ctx, e, job.TenantID, incident, false)
			if err != nil {
				return err
			}
			p, err := s.preference(ctx, e, job.TenantID, d.UserID)
			if err != nil {
				return err
			}
			if !incidents.CanRead(a, r) || visibility == "INTERNAL" && !incidents.CanReadNotes(a) || !p.SMTP || !kindEnabled(p, kind) {
				skip = "SKIPPED"
			}
		} else {
			summary = "Kontrolowana próba kanału SMTP"
			if !a.Permissions["admin.notifications.manage"] {
				skip = "SKIPPED"
			}
		}
		if err = e.QueryRowContext(ctx, `SELECT email FROM users WHERE id=?`, d.UserID).Scan(&to); err != nil {
			return err
		}
		if !validAddress(to) {
			skip = "SKIPPED"
		}
		c, err = s.config(ctx, e, job.TenantID)
		if err != nil {
			return err
		}
		if skip == "" && (!c.Enabled || c.Version == 0) {
			skip = "NOT_CONFIGURED"
		}
		if skip != "" {
			_, err = e.ExecContext(ctx, `UPDATE notification_mail_deliveries SET status=?,error_code=?,finished_at=UTC_TIMESTAMP(6) WHERE id=?`, skip, skip, input.ID)
			return err
		}
		if d.Attempts >= 100 {
			_, err = e.ExecContext(ctx, `UPDATE notification_mail_deliveries SET status='FAILED',error_code='SMTP_ATTEMPT_LIMIT',finished_at=UTC_TIMESTAMP(6) WHERE id=?`, input.ID)
			already = true
			return err
		}
		sequence = d.Attempts + 1
		_, err = e.ExecContext(ctx, `UPDATE notification_mail_attempts SET status='UNKNOWN',error_code='LEASE_LOST',finished_at=UTC_TIMESTAMP(6) WHERE delivery_id=? AND status='SENDING'`, input.ID)
		if err != nil {
			return err
		}
		_, err = e.ExecContext(ctx, `INSERT INTO notification_mail_attempts(delivery_id,attempt_number,status) VALUES(?,?,'SENDING')`, input.ID, sequence)
		if err != nil {
			return err
		}
		_, err = e.ExecContext(ctx, `UPDATE notification_mail_deliveries SET status='SENDING',attempt_count=?,provider_id=?,error_code='' WHERE id=?`, sequence, c.ProviderID, input.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	if skip != "" || already {
		return json.RawMessage(`{"sent":false}`), nil
	}
	secret, sendErr := s.unseal(job.TenantID, c.cipher)
	code := ""
	if sendErr != nil {
		code = "SMTP_KEY_UNAVAILABLE"
	} else {
		code = s.send(ctx, c, secret, to, summary, incident, input.ID)
	}
	// Do not include raw server replies/addresses/configuration in error logs.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	finishErr := s.tx.WithTx(finishCtx, nil, func(e database.Executor) error {
		var owned uint64
		if err := e.QueryRowContext(finishCtx, `SELECT id FROM jobs WHERE id=? AND status='RUNNING' AND lease_owner=? AND lease_expires_at>UTC_TIMESTAMP(6) FOR UPDATE`, job.ID, job.LeaseOwner).Scan(&owned); err != nil {
			return jobs.ErrLeaseLost
		}
		status := "DELIVERED"
		if code != "" {
			status = "FAILED"
			if job.AttemptCount < job.MaxAttempts {
				status = "QUEUED"
			}
		}
		_, err := e.ExecContext(finishCtx, `UPDATE notification_mail_deliveries SET status=?,error_code=?,finished_at=IF(?='QUEUED',NULL,UTC_TIMESTAMP(6)) WHERE id=? AND job_id=? AND attempt_count=?`, status, code, status, input.ID, job.ID, sequence)
		if err != nil {
			return err
		}
		attemptStatus := "DELIVERED"
		if code != "" {
			attemptStatus = "FAILED"
		}
		_, err = e.ExecContext(finishCtx, `UPDATE notification_mail_attempts SET status=?,error_code=?,finished_at=UTC_TIMESTAMP(6) WHERE delivery_id=? AND attempt_number=?`, attemptStatus, code, input.ID, sequence)
		return err
	})
	if finishErr != nil {
		return nil, finishErr
	}
	if code != "" {
		return nil, errors.New(code)
	}
	return json.RawMessage(`{"sent":true}`), nil
}

func (s *Service) send(ctx context.Context, c SMTPConfig, password, to, summary string, incident, id uint64) string {
	if c.TLSMode == "NONE" && !s.allowInsecure {
		return "SMTP_INSECURE_DISABLED"
	}
	deadline := time.Now().Add(15 * time.Second)
	if parent, ok := ctx.Deadline(); ok && parent.Before(deadline) {
		deadline = parent
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(c.Host, strconv.Itoa(c.Port)))
	if err != nil {
		return "SMTP_CONNECT_FAILED"
	}
	defer conn.Close()
	rawConn := conn
	_ = conn.SetDeadline(deadline)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			rawConn.Close()
		case <-done:
		}
	}()
	if c.TLSMode == "TLS" {
		secure := tls.Client(conn, &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12})
		if secure.HandshakeContext(ctx) != nil {
			return "SMTP_TLS_FAILED"
		}
		conn = secure
	}
	client, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		return "SMTP_PROTOCOL_FAILED"
	}
	defer client.Close()
	if c.TLSMode == "STARTTLS" {
		if err = client.StartTLS(&tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return "SMTP_TLS_FAILED"
		}
	}
	if c.Username != "" {
		if err = client.Auth(smtp.PlainAuth("", c.Username, password, c.Host)); err != nil {
			return "SMTP_AUTH_FAILED"
		}
	}
	if err = client.Mail(c.From); err != nil {
		return "SMTP_SENDER_REJECTED"
	}
	if err = client.Rcpt(to); err != nil {
		return "SMTP_RECIPIENT_REJECTED"
	}
	writer, err := client.Data()
	if err != nil {
		return "SMTP_DATA_REJECTED"
	}
	body := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: OpenServiceNOW notification\r\nMessage-ID: <opensnow-%d-%d@%s>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n", c.From, to, id, c.Version, strings.Split(c.From, "@")[1], summary)
	if incident > 0 {
		body += fmt.Sprintf("\r\nOtwórz incydent #%d w OpenServiceNOW. Dostęp wymaga aktywnej sesji i aktualnych uprawnień.\r\n", incident)
	}
	if _, err = io.WriteString(writer, body); err != nil {
		return "SMTP_RESULT_UNKNOWN"
	}
	if err = writer.Close(); err != nil {
		return "SMTP_RESULT_UNKNOWN"
	}
	_ = client.Quit()
	return ""
}
