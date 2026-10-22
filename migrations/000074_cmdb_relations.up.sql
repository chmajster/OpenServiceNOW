CREATE TABLE cmdb_relations (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 tenant_id BIGINT UNSIGNED NOT NULL,
 source_id BIGINT UNSIGNED NOT NULL,
 target_id BIGINT UNSIGNED NOT NULL,
 relation_type VARCHAR(20) NOT NULL,
 version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 UNIQUE KEY ci_relation_key(tenant_id,source_id,target_id,relation_type),
 KEY ci_relation_target(tenant_id,target_id,id),
 CONSTRAINT ci_relation_self CHECK(source_id<>target_id),
 CONSTRAINT ci_relation_type CHECK(relation_type IN('RUNS_ON','HOSTED_ON','DEPENDS_ON','CONNECTS_TO')),
 FOREIGN KEY(tenant_id,source_id) REFERENCES cmdb_cis(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,target_id) REFERENCES cmdb_cis(tenant_id,id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
