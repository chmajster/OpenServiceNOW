CREATE TABLE change_calendar_rules (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, tenant_id BIGINT UNSIGNED NOT NULL,
 ci_id BIGINT UNSIGNED NULL, kind VARCHAR(12) NOT NULL, title VARCHAR(160) NOT NULL,
 starts_at DATETIME(6) NOT NULL, ends_at DATETIME(6) NOT NULL, time_zone VARCHAR(64) NOT NULL,
 active BOOLEAN NOT NULL DEFAULT TRUE, version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 UNIQUE KEY change_calendar_rule_tenant(tenant_id,id), KEY change_calendar_rule_range(tenant_id,starts_at,ends_at),
 FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,ci_id) REFERENCES cmdb_cis(tenant_id,id),
 CHECK(kind IN('WINDOW','BLACKOUT')), CHECK(ends_at>starts_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
