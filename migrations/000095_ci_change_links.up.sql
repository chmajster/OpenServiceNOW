CREATE TABLE cmdb_change_links (
 tenant_id BIGINT UNSIGNED NOT NULL,
 ci_id BIGINT UNSIGNED NOT NULL,
 change_id BIGINT UNSIGNED NOT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 PRIMARY KEY(tenant_id,ci_id,change_id),
 KEY ci_change_reverse(tenant_id,change_id,ci_id),
 FOREIGN KEY(tenant_id,ci_id) REFERENCES cmdb_cis(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,change_id) REFERENCES changes(tenant_id,id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
