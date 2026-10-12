CREATE TABLE catalog_items (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 tenant_id BIGINT UNSIGNED NOT NULL,
 category_id BIGINT UNSIGNED NOT NULL,
 draft JSON NOT NULL,
 published_version BIGINT UNSIGNED NULL,
 version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 UNIQUE KEY item_tenant_id(tenant_id,id),
 FOREIGN KEY(tenant_id,category_id) REFERENCES catalog_categories(tenant_id,id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
