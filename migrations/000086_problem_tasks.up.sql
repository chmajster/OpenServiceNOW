CREATE TABLE problem_tasks (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 tenant_id BIGINT UNSIGNED NOT NULL,
 problem_id BIGINT UNSIGNED NOT NULL,
 number VARCHAR(40) NOT NULL,
 title VARCHAR(160) NOT NULL,
 instructions TEXT NOT NULL,
 required BOOLEAN NOT NULL DEFAULT TRUE,
 state VARCHAR(24) NOT NULL DEFAULT 'OPEN',
 result TEXT NOT NULL,
 version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 UNIQUE KEY problem_task_tenant_id(tenant_id,id),
 UNIQUE KEY problem_task_number(tenant_id,number),
 KEY problem_task_parent(tenant_id,problem_id,id),
 FOREIGN KEY(tenant_id,problem_id) REFERENCES problems(tenant_id,id) ON DELETE CASCADE,
 CHECK(state IN('OPEN','IN_PROGRESS','COMPLETED','CANCELLED'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
