CREATE TABLE incident_major_links (
 tenant_id BIGINT UNSIGNED NOT NULL,
 incident_id BIGINT UNSIGNED NOT NULL,
 target_type ENUM('SERVICE','CI') NOT NULL,
 target_id BIGINT UNSIGNED NOT NULL,
 added_by BIGINT UNSIGNED NOT NULL,
 created_at DATETIME(6) NOT NULL,
 PRIMARY KEY(tenant_id,incident_id,target_type,target_id),
 KEY incident_major_link_target(tenant_id,target_type,target_id,incident_id),
 CONSTRAINT incident_major_link_case FOREIGN KEY(tenant_id,incident_id) REFERENCES incident_major_cases(tenant_id,incident_id) ON DELETE CASCADE,
 CONSTRAINT incident_major_link_actor FOREIGN KEY(tenant_id,added_by) REFERENCES tenant_memberships(tenant_id,user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
