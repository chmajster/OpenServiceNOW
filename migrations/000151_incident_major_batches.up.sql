CREATE TABLE incident_major_batches (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 tenant_id BIGINT UNSIGNED NOT NULL,
 incident_id BIGINT UNSIGNED NOT NULL,
 actor_user_id BIGINT UNSIGNED NOT NULL,
 results JSON NOT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 KEY incident_major_batch_case(tenant_id,incident_id,id),
 CONSTRAINT incident_major_batch_case_fk FOREIGN KEY(tenant_id,incident_id) REFERENCES incident_major_cases(tenant_id,incident_id) ON DELETE CASCADE,
 CONSTRAINT incident_major_batch_actor FOREIGN KEY(tenant_id,actor_user_id) REFERENCES tenant_memberships(tenant_id,user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
