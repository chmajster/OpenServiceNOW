ALTER TABLE attachments DROP FOREIGN KEY fk_attachment_message, DROP CHECK chk_attachment_visibility, DROP COLUMN message_id, DROP COLUMN visibility;
