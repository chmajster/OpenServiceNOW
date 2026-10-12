CREATE TABLE workflow_decisions (
 tenant_id BIGINT UNSIGNED NOT NULL,
 execution_id BIGINT UNSIGNED NOT NULL,
 node_id VARCHAR(48) NOT NULL,
 user_id BIGINT UNSIGNED NOT NULL,
 group_id BIGINT UNSIGNED NULL,
 decision VARCHAR(24) NOT NULL DEFAULT 'PENDING',
 reason TEXT NOT NULL,
 decided_at DATETIME(6) NULL,
 PRIMARY KEY(tenant_id,execution_id,node_id,user_id),
 KEY workflow_inbox(tenant_id,user_id,execution_id),
 FOREIGN KEY(tenant_id,execution_id,node_id) REFERENCES workflow_steps(tenant_id,execution_id,node_id) ON DELETE CASCADE,
 FOREIGN KEY(user_id) REFERENCES users(id),
 FOREIGN KEY(tenant_id,group_id) REFERENCES `groups`(tenant_id,id) ON DELETE CASCADE,
 CHECK(decision IN('PENDING','APPROVED','REJECTED'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
