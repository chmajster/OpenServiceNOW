CREATE TABLE incident_major_updates (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 tenant_id BIGINT UNSIGNED NOT NULL,
 incident_id BIGINT UNSIGNED NOT NULL,
 audience ENUM('OPERATORS','REQUESTERS') NOT NULL,
 body VARCHAR(20000) NOT NULL,
 actor_user_id BIGINT UNSIGNED NOT NULL,
 delivery_state ENUM('PENDING','IN_APP_DELIVERED','SMTP_QUEUED','NO_RECIPIENTS','FAILED') NOT NULL DEFAULT 'PENDING',
 source_event_id BIGINT UNSIGNED NULL,
 occurred_at DATETIME(6) NOT NULL,
 KEY incident_major_update_history(tenant_id,incident_id,audience,id),
 KEY incident_major_update_event(tenant_id,source_event_id),
 CONSTRAINT incident_major_update_case FOREIGN KEY(tenant_id,incident_id) REFERENCES incident_major_cases(tenant_id,incident_id) ON DELETE CASCADE,
 CONSTRAINT incident_major_update_actor FOREIGN KEY(tenant_id,actor_user_id) REFERENCES tenant_memberships(tenant_id,user_id),
 CONSTRAINT incident_major_update_event_fk FOREIGN KEY(source_event_id) REFERENCES outbox_events(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
