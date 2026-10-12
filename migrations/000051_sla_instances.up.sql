CREATE TABLE sla_instances (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 tenant_id BIGINT UNSIGNED NOT NULL,
 incident_id BIGINT UNSIGNED NOT NULL,
 kind ENUM('response','resolution') NOT NULL,
 cycle BIGINT UNSIGNED NOT NULL,
 policy_id BIGINT UNSIGNED NOT NULL,
 policy_version BIGINT UNSIGNED NOT NULL,
 version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 state JSON NOT NULL,
 UNIQUE KEY uq_sla_cycle(tenant_id,incident_id,kind,cycle),
 KEY ix_sla_incident(tenant_id,incident_id,id),
 FOREIGN KEY(tenant_id,incident_id) REFERENCES incidents(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(policy_id,policy_version) REFERENCES sla_definition_versions(definition_id,version) ON DELETE CASCADE
);
