CREATE TABLE incident_major_decisions (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 tenant_id BIGINT UNSIGNED NOT NULL,
 incident_id BIGINT UNSIGNED NOT NULL,
 case_version BIGINT UNSIGNED NOT NULL,
 decision_type ENUM('NOMINATED','PROMOTED','REJECTED','DEMOTED','RESTORED','REOPENED') NOT NULL,
 reason VARCHAR(2000) NOT NULL,
 actor_user_id BIGINT UNSIGNED NOT NULL,
 occurred_at DATETIME(6) NOT NULL,
 KEY incident_major_decision_timeline(tenant_id,incident_id,id),
 CONSTRAINT incident_major_decision_case FOREIGN KEY(tenant_id,incident_id) REFERENCES incident_major_cases(tenant_id,incident_id) ON DELETE CASCADE,
 CONSTRAINT incident_major_decision_actor FOREIGN KEY(tenant_id,actor_user_id) REFERENCES tenant_memberships(tenant_id,user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
