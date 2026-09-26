CREATE TABLE job_logs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    job_id BIGINT UNSIGNED NOT NULL,
    attempt_number INT UNSIGNED NOT NULL,
    level VARCHAR(10) NOT NULL,
    message VARCHAR(1000) NOT NULL,
    fields JSON NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_job_logs_job (job_id, id),
    CONSTRAINT chk_job_logs_level CHECK (level IN ('DEBUG','INFO','WARN','ERROR')),
    CONSTRAINT fk_job_logs_job FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
