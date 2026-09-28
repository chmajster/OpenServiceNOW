CREATE TABLE work_assignments (
    tenant_id BIGINT UNSIGNED NOT NULL,
    object_type VARCHAR(100) NOT NULL,
    object_id VARCHAR(128) NOT NULL,
    assigned_to BIGINT UNSIGNED NULL,
    assignment_group_id BIGINT UNSIGNED NULL,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (tenant_id, object_type, object_id),
    KEY idx_work_assignments_user (tenant_id, assigned_to, object_type, object_id),
    KEY idx_work_assignments_group (tenant_id, assignment_group_id, object_type, object_id),
    CONSTRAINT chk_work_assignments_version CHECK (version >= 1),
    CONSTRAINT fk_work_assignments_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT fk_work_assignments_user FOREIGN KEY (assigned_to) REFERENCES users(id) ON DELETE SET NULL,
    CONSTRAINT fk_work_assignments_group FOREIGN KEY (assignment_group_id) REFERENCES groups(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
