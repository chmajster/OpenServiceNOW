CREATE TABLE job_attempts (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    job_id BIGINT UNSIGNED NOT NULL,
    attempt_number INT UNSIGNED NOT NULL,
    worker_id VARCHAR(190) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'RUNNING',
    started_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    finished_at DATETIME(6) NULL,
    error_message VARCHAR(4000) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_job_attempt_number (job_id, attempt_number),
    KEY idx_job_attempts_job_id (job_id, id),
    CONSTRAINT chk_job_attempt_status CHECK (status IN ('RUNNING','COMPLETED','FAILED','CANCELLED','LOST')),
    CONSTRAINT fk_job_attempts_job FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
