CREATE TABLE installation_state (
    id BIGINT UNSIGNED NOT NULL,
    completed_at DATETIME(6) NULL,
    PRIMARY KEY (id),
    CONSTRAINT chk_installation_state_singleton CHECK (id = 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
SELECT CAST(1 AS UNSIGNED) AS id,
    CASE WHEN EXISTS (SELECT 1 FROM users) OR EXISTS (SELECT 1 FROM tenants)
        THEN CURRENT_TIMESTAMP(6) ELSE NULL END AS completed_at;
