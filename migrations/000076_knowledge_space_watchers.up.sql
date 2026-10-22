CREATE TABLE knowledge_space_watchers (
 tenant_id BIGINT UNSIGNED NOT NULL,
 space_id BIGINT UNSIGNED NOT NULL,
 user_id BIGINT UNSIGNED NOT NULL,
 created_at DATETIME(6) NOT NULL,
 PRIMARY KEY (tenant_id,space_id,user_id),
 KEY idx_knowledge_watchers_user (tenant_id,user_id,space_id),
 CONSTRAINT fk_knowledge_watchers_space FOREIGN KEY (tenant_id,space_id) REFERENCES knowledge_spaces(tenant_id,id) ON DELETE CASCADE,
 CONSTRAINT fk_knowledge_watchers_member FOREIGN KEY (tenant_id,user_id) REFERENCES tenant_memberships(tenant_id,user_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
