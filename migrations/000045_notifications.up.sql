CREATE TABLE notifications (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 tenant_id BIGINT UNSIGNED NOT NULL, user_id BIGINT UNSIGNED NOT NULL,
 source_event_id BIGINT UNSIGNED NOT NULL, incident_id BIGINT UNSIGNED NOT NULL,
 kind VARCHAR(30) NOT NULL, visibility VARCHAR(20) NOT NULL,
 summary VARCHAR(300) NOT NULL, in_app_enabled BOOLEAN NOT NULL,
 read_at DATETIME(6) NULL, occurred_at DATETIME(6) NOT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 UNIQUE KEY uq_notification_effect(source_event_id,user_id),
 KEY idx_notification_user(tenant_id,user_id,id),
 FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
 FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
 FOREIGN KEY(source_event_id) REFERENCES outbox_events(id) ON DELETE CASCADE,
 FOREIGN KEY(incident_id) REFERENCES incidents(id) ON DELETE CASCADE,
 CHECK(visibility IN ('PUBLIC','INTERNAL'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
