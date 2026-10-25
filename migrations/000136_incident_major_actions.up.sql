CREATE TABLE incident_major_actions (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 tenant_id BIGINT UNSIGNED NOT NULL,
 incident_id BIGINT UNSIGNED NOT NULL,
 description VARCHAR(4000) NOT NULL,
 owner_user_id BIGINT UNSIGNED NULL,
 due_at DATETIME(6) NOT NULL,
 status ENUM('OPEN','IN_PROGRESS','DONE','CANCELLED') NOT NULL DEFAULT 'OPEN',
 version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 created_by BIGINT UNSIGNED NOT NULL,
 created_at DATETIME(6) NOT NULL,
 updated_at DATETIME(6) NOT NULL,
 KEY incident_major_action_queue(tenant_id,owner_user_id,status,due_at,id),
 KEY incident_major_action_case(tenant_id,incident_id,status,id),
 CONSTRAINT incident_major_action_case_fk FOREIGN KEY(tenant_id,incident_id) REFERENCES incident_major_cases(tenant_id,incident_id) ON DELETE CASCADE,
 CONSTRAINT incident_major_action_owner FOREIGN KEY(tenant_id,owner_user_id) REFERENCES tenant_memberships(tenant_id,user_id),
 CONSTRAINT incident_major_action_creator FOREIGN KEY(tenant_id,created_by) REFERENCES tenant_memberships(tenant_id,user_id),
 CONSTRAINT incident_major_action_version CHECK(version>=1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
