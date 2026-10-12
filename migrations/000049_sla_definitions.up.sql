CREATE TABLE sla_definitions (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 tenant_id BIGINT UNSIGNED NOT NULL,
 kind ENUM('calendar','policy') NOT NULL,
 version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 request_key VARCHAR(100) NOT NULL,
 creation_hash BINARY(32) NOT NULL,
 created_at DATETIME(6) NOT NULL,
 UNIQUE KEY uq_sla_definition_scope(tenant_id,id),
 UNIQUE KEY uq_sla_definition_request(tenant_id,kind,request_key),
 KEY ix_sla_definitions_list(tenant_id,kind,id),
 FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);
