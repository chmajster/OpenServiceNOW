CREATE TABLE requested_items (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 tenant_id BIGINT UNSIGNED NOT NULL,
 request_id BIGINT UNSIGNED NOT NULL,
 number VARCHAR(40) NOT NULL,
 catalog_item_id BIGINT UNSIGNED NOT NULL,
 catalog_version BIGINT UNSIGNED NOT NULL,
 snapshot JSON NOT NULL,
 form_values JSON NOT NULL,
 UNIQUE KEY requested_item_tenant_id(tenant_id,id),
 UNIQUE KEY requested_number(tenant_id,number),
 FOREIGN KEY(tenant_id,request_id) REFERENCES service_requests(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,catalog_item_id,catalog_version) REFERENCES catalog_item_versions(tenant_id,item_id,version) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
