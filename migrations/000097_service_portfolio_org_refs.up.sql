CREATE TABLE service_portfolio_org_refs (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 tenant_id BIGINT UNSIGNED NOT NULL,
 kind ENUM('ORG_UNIT','DEPARTMENT','COST_CENTER') NOT NULL,
 ref_key VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 name VARCHAR(160) NOT NULL,
 active BOOLEAN NOT NULL DEFAULT TRUE,
 version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
 UNIQUE KEY service_portfolio_org_ref_tenant_id(tenant_id,id),
 UNIQUE KEY service_portfolio_org_ref_key(tenant_id,kind,ref_key),
 KEY service_portfolio_org_ref_list(tenant_id,kind,active,id),
 CONSTRAINT service_portfolio_org_ref_tenant FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
