CREATE TABLE change_problem_links (
 tenant_id BIGINT UNSIGNED NOT NULL, change_id BIGINT UNSIGNED NOT NULL, problem_id BIGINT UNSIGNED NOT NULL,
 PRIMARY KEY(tenant_id,change_id,problem_id), KEY problem_changes(tenant_id,problem_id,change_id),
 FOREIGN KEY(tenant_id,change_id) REFERENCES changes(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,problem_id) REFERENCES problems(tenant_id,id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
