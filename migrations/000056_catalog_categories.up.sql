CREATE TABLE catalog_categories (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 tenant_id BIGINT UNSIGNED NOT NULL,
 catalog_id BIGINT UNSIGNED NOT NULL,
 name VARCHAR(160) NOT NULL,
 description TEXT NOT NULL,
 version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 UNIQUE KEY category_tenant_id(tenant_id,id),
 FOREIGN KEY(tenant_id,catalog_id) REFERENCES service_catalogs(tenant_id,id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
