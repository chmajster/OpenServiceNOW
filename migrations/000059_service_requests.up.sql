CREATE TABLE service_requests (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 tenant_id BIGINT UNSIGNED NOT NULL,
 number VARCHAR(40) NOT NULL,
 requester_id BIGINT UNSIGNED NOT NULL,
 state VARCHAR(24) NOT NULL DEFAULT 'SUBMITTED',
 reason TEXT NOT NULL,
 version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 created_at DATETIME(6) NOT NULL,
 updated_at DATETIME(6) NOT NULL,
 UNIQUE KEY request_tenant_id(tenant_id,id),
 UNIQUE KEY request_number(tenant_id,number),
 KEY request_own(tenant_id,requester_id,id),
 FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,requester_id) REFERENCES tenant_memberships(tenant_id,user_id) ON DELETE CASCADE,
 CHECK(state IN('SUBMITTED','IN_PROGRESS','FULFILLED','CLOSED','CANCELLED'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
