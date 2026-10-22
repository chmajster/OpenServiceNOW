CREATE TABLE cmdb_incident_links (
 tenant_id BIGINT UNSIGNED NOT NULL,
 ci_id BIGINT UNSIGNED NOT NULL,
 incident_id BIGINT UNSIGNED NOT NULL,
 PRIMARY KEY(tenant_id,ci_id,incident_id),
 KEY incident_ci_links(tenant_id,incident_id,ci_id),
 FOREIGN KEY(tenant_id,ci_id) REFERENCES cmdb_cis(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,incident_id) REFERENCES incidents(tenant_id,id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
