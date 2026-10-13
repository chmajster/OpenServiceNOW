ALTER TABLE workflow_executions
 MODIFY request_id BIGINT UNSIGNED NULL, MODIFY requested_item_id BIGINT UNSIGNED NULL,
 ADD domain_type VARCHAR(12) NOT NULL DEFAULT 'REQUEST', ADD change_id BIGINT UNSIGNED NULL,
 ADD subject_version BIGINT UNSIGNED NULL, ADD subject_revision BIGINT UNSIGNED NULL, ADD subject_snapshot JSON NULL,
 ADD UNIQUE KEY workflow_change_revision(tenant_id,change_id,subject_revision),
 ADD CONSTRAINT workflow_change_subject_fk FOREIGN KEY(tenant_id,change_id) REFERENCES changes(tenant_id,id) ON DELETE CASCADE,
 ADD CONSTRAINT workflow_subject CHECK((domain_type='REQUEST' AND request_id IS NOT NULL AND requested_item_id IS NOT NULL AND change_id IS NULL AND subject_version IS NULL AND subject_revision IS NULL AND subject_snapshot IS NULL) OR (domain_type='CHANGE' AND change_id IS NOT NULL AND request_id IS NULL AND requested_item_id IS NULL AND subject_version IS NOT NULL AND subject_revision IS NOT NULL AND subject_snapshot IS NOT NULL));
