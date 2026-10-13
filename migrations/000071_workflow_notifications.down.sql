ALTER TABLE notifications DROP CHECK notification_object, DROP FOREIGN KEY notification_workflow, DROP COLUMN execution_id, MODIFY incident_id BIGINT UNSIGNED NOT NULL;
