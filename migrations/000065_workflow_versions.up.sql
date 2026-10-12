CREATE TABLE workflow_versions (
 tenant_id BIGINT UNSIGNED NOT NULL,
 definition_id BIGINT UNSIGNED NOT NULL,
 version BIGINT UNSIGNED NOT NULL,
 config JSON NOT NULL,
 published_by BIGINT UNSIGNED NOT NULL,
 published_at DATETIME(6) NOT NULL,
 PRIMARY KEY(tenant_id,definition_id,version),
 FOREIGN KEY(tenant_id,definition_id) REFERENCES workflow_definitions(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(published_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
