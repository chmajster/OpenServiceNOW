CREATE TABLE problem_incident_links (
 tenant_id BIGINT UNSIGNED NOT NULL,
 problem_id BIGINT UNSIGNED NOT NULL,
 incident_id BIGINT UNSIGNED NOT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 PRIMARY KEY(tenant_id,problem_id,incident_id),
 KEY problem_incident_reverse(tenant_id,incident_id,problem_id),
 FOREIGN KEY(tenant_id,problem_id) REFERENCES problems(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,incident_id) REFERENCES incidents(tenant_id,id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
