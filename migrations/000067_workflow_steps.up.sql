CREATE TABLE workflow_steps (
 tenant_id BIGINT UNSIGNED NOT NULL,
 execution_id BIGINT UNSIGNED NOT NULL,
 node_id VARCHAR(48) NOT NULL,
 state VARCHAR(24) NOT NULL,
 version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 result JSON NOT NULL,
 started_at DATETIME(6) NOT NULL,
 deadline DATETIME(6) NULL,
 finished_at DATETIME(6) NULL,
 PRIMARY KEY(tenant_id,execution_id,node_id),
 FOREIGN KEY(tenant_id,execution_id) REFERENCES workflow_executions(tenant_id,id) ON DELETE CASCADE,
 CHECK(state IN('WAITING','COMPLETED','CANCELLED'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
