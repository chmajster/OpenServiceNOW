ALTER TABLE sessions ADD INDEX idx_sessions_account (user_id, revoked_at, id, expires_at);
