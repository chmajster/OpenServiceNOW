CREATE TABLE workspace_views (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    owner_user_id BIGINT UNSIGNED NOT NULL,
    name VARCHAR(80) NOT NULL,
    shared BOOLEAN NOT NULL DEFAULT FALSE,
    definition JSON NOT NULL,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_workspace_views_tenant_id (tenant_id, id),
    UNIQUE KEY uq_workspace_views_owner_name (tenant_id, owner_user_id, name),
    KEY idx_workspace_views_shared (tenant_id, shared, updated_at),
    CONSTRAINT chk_workspace_views_version CHECK (version >= 1),
    CONSTRAINT fk_workspace_views_membership FOREIGN KEY (tenant_id, owner_user_id) REFERENCES tenant_memberships(tenant_id, user_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
