-- Stop application writers during migrations. Existing rows never become notification eligible.
ALTER TABLE outbox_events ADD COLUMN notification_eligible BOOLEAN NOT NULL DEFAULT FALSE;
