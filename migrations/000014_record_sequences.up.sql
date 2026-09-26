CREATE TABLE record_sequences (
    tenant_id BIGINT UNSIGNED NOT NULL,
    record_type VARCHAR(64) NOT NULL,
    prefix VARCHAR(16) NOT NULL,
    width TINYINT UNSIGNED NOT NULL DEFAULT 7,
    next_value BIGINT UNSIGNED NOT NULL DEFAULT 1,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (tenant_id, record_type),
    CONSTRAINT chk_record_sequences_width CHECK (width BETWEEN 1 AND 20),
    CONSTRAINT chk_record_sequences_next CHECK (next_value >= 1),
    CONSTRAINT fk_record_sequences_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
