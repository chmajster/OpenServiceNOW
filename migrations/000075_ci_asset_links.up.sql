CREATE TABLE cmdb_ci_assets (
 tenant_id BIGINT UNSIGNED NOT NULL,
 ci_id BIGINT UNSIGNED NOT NULL,
 asset_id BIGINT UNSIGNED NOT NULL,
 PRIMARY KEY(tenant_id,ci_id),
 UNIQUE KEY asset_ci_key(tenant_id,asset_id),
 FOREIGN KEY(tenant_id,ci_id) REFERENCES cmdb_cis(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,asset_id) REFERENCES assets(tenant_id,id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
