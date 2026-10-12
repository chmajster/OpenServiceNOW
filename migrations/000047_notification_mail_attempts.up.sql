CREATE TABLE notification_mail_attempts (
 delivery_id BIGINT UNSIGNED NOT NULL, attempt_number INT UNSIGNED NOT NULL,
 status VARCHAR(30) NOT NULL, error_code VARCHAR(100) NOT NULL DEFAULT '',
 started_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), finished_at DATETIME(6) NULL,
 PRIMARY KEY(delivery_id,attempt_number),
 FOREIGN KEY(delivery_id) REFERENCES notification_mail_deliveries(id) ON DELETE CASCADE
) ENGINE=InnoDB;
