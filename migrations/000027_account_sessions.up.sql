ALTER TABLE sessions ADD INDEX idx_sessions_account (user_id, revoked_at, expires_at, id);
