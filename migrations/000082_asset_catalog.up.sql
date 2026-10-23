CREATE TABLE asset_catalog_values (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 tenant_id BIGINT UNSIGNED NOT NULL,
 kind VARCHAR(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 value VARCHAR(160) NOT NULL,
 manufacturer VARCHAR(160) NOT NULL DEFAULT '',
 active BOOLEAN NOT NULL DEFAULT TRUE,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 UNIQUE KEY asset_catalog_unique(tenant_id,kind,manufacturer,value),
 KEY asset_catalog_search(tenant_id,kind,active,id),
 CONSTRAINT asset_catalog_kind CHECK(kind IN('MANUFACTURER','MODEL','LOCATION')),
 CONSTRAINT asset_catalog_manufacturer CHECK(kind='MODEL' OR manufacturer=''),
 FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
