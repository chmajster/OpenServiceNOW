CREATE TABLE sla_history (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 tenant_id BIGINT UNSIGNED NOT NULL,
 instance_id BIGINT UNSIGNED NOT NULL,
 source_key VARCHAR(150) NOT NULL,
 entry JSON NOT NULL,
 UNIQUE KEY uq_sla_history(instance_id,source_key),
 KEY ix_sla_history(tenant_id,instance_id,id),
 FOREIGN KEY(instance_id) REFERENCES sla_instances(id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);
