ALTER TABLE notifications
 MODIFY incident_id BIGINT UNSIGNED NULL,
 ADD execution_id BIGINT UNSIGNED NULL,
 ADD CONSTRAINT notification_workflow FOREIGN KEY(tenant_id,execution_id) REFERENCES workflow_executions(tenant_id,id) ON DELETE CASCADE,
 ADD CONSTRAINT notification_object CHECK((incident_id IS NOT NULL AND execution_id IS NULL) OR (incident_id IS NULL AND execution_id IS NOT NULL));
