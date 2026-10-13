CREATE TABLE inventory_import_rows (
 tenant_id BIGINT UNSIGNED NOT NULL,
 import_id BIGINT UNSIGNED NOT NULL,
 source_row INT UNSIGNED NOT NULL,
 identity_value VARCHAR(128) NOT NULL,
 action VARCHAR(20) NOT NULL,
 state VARCHAR(20) NOT NULL DEFAULT 'PENDING',
 record_id BIGINT UNSIGNED NULL,
 expected_version BIGINT UNSIGNED NOT NULL DEFAULT 0,
 before_value JSON NOT NULL,
 after_value JSON NOT NULL,
 error_code VARCHAR(80) NOT NULL DEFAULT '',
 error_message VARCHAR(500) NOT NULL DEFAULT '',
 PRIMARY KEY(tenant_id,import_id,source_row),
 CONSTRAINT inventory_import_row_state CHECK(state IN('PENDING','APPLIED','ERROR','CONFLICT')),
 FOREIGN KEY(tenant_id,import_id) REFERENCES inventory_imports(tenant_id,id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
