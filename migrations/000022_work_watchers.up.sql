CREATE TABLE work_watchers (
    tenant_id BIGINT UNSIGNED NOT NULL,
    object_type VARCHAR(100) NOT NULL,
    object_id VARCHAR(128) NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    created_by BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (tenant_id, object_type, object_id, user_id),
    KEY idx_work_watchers_user (tenant_id, user_id, object_type, object_id),
    CONSTRAINT fk_work_watchers_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT fk_work_watchers_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_work_watchers_creator FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
