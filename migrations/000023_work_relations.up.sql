CREATE TABLE work_relations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    source_type VARCHAR(100) NOT NULL,
    source_id VARCHAR(128) NOT NULL,
    relation_type VARCHAR(64) NOT NULL,
    target_type VARCHAR(100) NOT NULL,
    target_id VARCHAR(128) NOT NULL,
    created_by BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_work_relation (tenant_id, source_type, source_id, relation_type, target_type, target_id),
    KEY idx_work_relations_target (tenant_id, target_type, target_id, id),
    CONSTRAINT fk_work_relations_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT fk_work_relations_creator FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
