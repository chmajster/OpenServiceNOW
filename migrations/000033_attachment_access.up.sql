ALTER TABLE attachments
    ADD COLUMN visibility VARCHAR(20) NOT NULL DEFAULT 'INTERNAL',
    ADD COLUMN message_id BIGINT UNSIGNED NULL,
    ADD CONSTRAINT chk_attachment_visibility CHECK (visibility IN ('PUBLIC','INTERNAL')),
    ADD CONSTRAINT fk_attachment_message FOREIGN KEY (message_id) REFERENCES work_messages(id) ON DELETE RESTRICT;
