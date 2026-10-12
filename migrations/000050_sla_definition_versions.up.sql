CREATE TABLE sla_definition_versions (
 tenant_id BIGINT UNSIGNED NOT NULL,
 definition_id BIGINT UNSIGNED NOT NULL,
 version BIGINT UNSIGNED NOT NULL,
 effective_at DATETIME(6) NOT NULL,
 config JSON NOT NULL,
 PRIMARY KEY(definition_id,version),
 KEY ix_sla_effective(tenant_id,effective_at,definition_id),
 FOREIGN KEY(tenant_id,definition_id) REFERENCES sla_definitions(tenant_id,id) ON DELETE CASCADE
);
