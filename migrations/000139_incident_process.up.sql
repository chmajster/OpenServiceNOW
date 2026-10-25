ALTER TABLE incidents ADD  service_id BIGINT UNSIGNED NULL,
 ADD  location VARCHAR(160) NOT NULL DEFAULT '',
 ADD  template_key VARCHAR(48) NOT NULL DEFAULT '',
 ADD  template_version BIGINT UNSIGNED NOT NULL DEFAULT 0,
 ADD  process_version BIGINT UNSIGNED NOT NULL DEFAULT 0,
 ADD  process_policy JSON NULL,
 ADD  assignment_origin VARCHAR(48) NOT NULL DEFAULT '',
 ADD  assignment_reason VARCHAR(2000) NOT NULL DEFAULT '', ADD KEY incident_service_queue(tenant_id,service_id,state,id), ADD CONSTRAINT incident_service_fk FOREIGN KEY(tenant_id,service_id) REFERENCES service_portfolio_items(tenant_id,id);
