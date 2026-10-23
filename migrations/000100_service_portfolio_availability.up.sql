CREATE TABLE service_portfolio_component_availability (
 tenant_id BIGINT UNSIGNED NOT NULL,
 node_kind ENUM('ITEM','CI') NOT NULL,
 node_id BIGINT UNSIGNED NOT NULL,
 state ENUM('AVAILABLE','DEGRADED','UNAVAILABLE','UNKNOWN') NOT NULL,
 source ENUM('MANUAL') NOT NULL DEFAULT 'MANUAL',
 note VARCHAR(500) NOT NULL DEFAULT '',
 version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 observed_by BIGINT UNSIGNED NOT NULL,
 observed_at DATETIME(6) NOT NULL,
 PRIMARY KEY(tenant_id,node_kind,node_id),
 KEY service_portfolio_availability_observed(tenant_id,observed_at),
 CONSTRAINT service_portfolio_availability_tenant FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
 CONSTRAINT service_portfolio_availability_actor FOREIGN KEY(observed_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
