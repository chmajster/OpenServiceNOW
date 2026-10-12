CREATE TABLE sla_work (
 tenant_id BIGINT UNSIGNED NOT NULL,
 incident_id BIGINT UNSIGNED NOT NULL,
 next_check_at DATETIME(6) NULL,
 checkpoint JSON NOT NULL,
 PRIMARY KEY(tenant_id,incident_id),
 KEY ix_sla_next_check(next_check_at,tenant_id,incident_id),
 FOREIGN KEY(tenant_id,incident_id) REFERENCES incidents(tenant_id,id) ON DELETE CASCADE
);
