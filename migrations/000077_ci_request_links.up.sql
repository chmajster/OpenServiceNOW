CREATE TABLE cmdb_request_links (
 tenant_id BIGINT UNSIGNED NOT NULL,
 ci_id BIGINT UNSIGNED NOT NULL,
 request_id BIGINT UNSIGNED NOT NULL,
 PRIMARY KEY(tenant_id,ci_id,request_id),
 KEY request_ci_links(tenant_id,request_id,ci_id),
 FOREIGN KEY(tenant_id,ci_id) REFERENCES cmdb_cis(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,request_id) REFERENCES service_requests(tenant_id,id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
