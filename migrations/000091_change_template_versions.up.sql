CREATE TABLE change_template_versions (
 tenant_id BIGINT UNSIGNED NOT NULL, template_id BIGINT UNSIGNED NOT NULL,
 version BIGINT UNSIGNED NOT NULL, config JSON NOT NULL,
 approved_by BIGINT UNSIGNED NOT NULL, approved_at DATETIME(6) NOT NULL,
 PRIMARY KEY(tenant_id,template_id,version),
 FOREIGN KEY(tenant_id,template_id) REFERENCES change_templates(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(approved_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
