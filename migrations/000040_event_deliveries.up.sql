CREATE TABLE event_deliveries (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    event_id BIGINT UNSIGNED NOT NULL,
    consumer_id VARCHAR(100) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    attempt_count INT UNSIGNED NOT NULL DEFAULT 0,
    max_attempts INT UNSIGNED NOT NULL,
    available_at DATETIME(6) NOT NULL,
    lease_owner VARCHAR(190) NULL,
    lease_expires_at DATETIME(6) NULL,
    last_error VARCHAR(1000) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    finished_at DATETIME(6) NULL,
    UNIQUE KEY uq_event_consumer(event_id,consumer_id),
    KEY idx_delivery_claim(status,available_at,lease_expires_at,id),
    CONSTRAINT fk_delivery_event FOREIGN KEY(event_id) REFERENCES outbox_events(id) ON DELETE CASCADE,
    CONSTRAINT chk_delivery_status CHECK(status IN ('PENDING','DELIVERING','DELIVERED','FAILED','SKIPPED')),
    CONSTRAINT chk_delivery_attempts CHECK(max_attempts BETWEEN 1 AND 100 AND attempt_count<=max_attempts)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
