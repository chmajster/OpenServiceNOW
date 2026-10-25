CREATE TABLE workspace_export_items (
    tenant_id BIGINT UNSIGNED NOT NULL,
    job_id BIGINT UNSIGNED NOT NULL,
    object_type VARCHAR(100) NOT NULL,
    object_id VARCHAR(128) NOT NULL,
    PRIMARY KEY (tenant_id, job_id, object_type, object_id),
    KEY idx_workspace_export_item_ref (tenant_id, object_type, object_id, job_id),
    CONSTRAINT fk_workspace_export_item_job FOREIGN KEY (tenant_id, job_id) REFERENCES workspace_exports(tenant_id, job_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
