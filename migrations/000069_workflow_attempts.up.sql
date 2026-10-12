CREATE TABLE workflow_attempts (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 tenant_id BIGINT UNSIGNED NOT NULL,
 execution_id BIGINT UNSIGNED NOT NULL,
 node_id VARCHAR(48) NOT NULL,
 execution_version BIGINT UNSIGNED NOT NULL,
 outcome VARCHAR(24) NOT NULL,
 error_code VARCHAR(64) NOT NULL DEFAULT '',
 occurred_at DATETIME(6) NOT NULL,
 KEY workflow_attempt_history(tenant_id,execution_id,id),
 FOREIGN KEY(tenant_id,execution_id) REFERENCES workflow_executions(tenant_id,id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
