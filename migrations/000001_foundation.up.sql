CREATE TABLE platform_metadata (
    id BIGINT UNSIGNED NOT NULL,
    installed_at DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT chk_platform_metadata_singleton CHECK (id = 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
SELECT CAST(1 AS UNSIGNED) AS id, CURRENT_TIMESTAMP(6) AS installed_at;
