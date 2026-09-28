CREATE TABLE work_messages (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    object_type VARCHAR(100) NOT NULL,
    object_id VARCHAR(128) NOT NULL,
    message_type VARCHAR(20) NOT NULL,
    body MEDIUMTEXT NOT NULL,
    author_user_id BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_work_messages_object (tenant_id, object_type, object_id, id),
    KEY idx_work_messages_author (tenant_id, author_user_id, id),
    CONSTRAINT chk_work_messages_type CHECK (message_type IN ('COMMENT','WORK_NOTE')),
    CONSTRAINT fk_work_messages_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT fk_work_messages_author FOREIGN KEY (author_user_id) REFERENCES users(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
