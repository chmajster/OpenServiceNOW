CREATE TABLE incident_major_teams (
 tenant_id BIGINT UNSIGNED NOT NULL,
 incident_id BIGINT UNSIGNED NOT NULL,
 group_id BIGINT UNSIGNED NOT NULL,
 added_by BIGINT UNSIGNED NOT NULL,
 created_at DATETIME(6) NOT NULL,
 PRIMARY KEY(tenant_id,incident_id,group_id),
 KEY incident_major_team_group(tenant_id,group_id,incident_id),
 CONSTRAINT incident_major_team_case FOREIGN KEY(tenant_id,incident_id) REFERENCES incident_major_cases(tenant_id,incident_id) ON DELETE CASCADE,
 CONSTRAINT incident_major_team_group_fk FOREIGN KEY(tenant_id,group_id) REFERENCES `groups`(tenant_id,id),
 CONSTRAINT incident_major_team_actor FOREIGN KEY(tenant_id,added_by) REFERENCES tenant_memberships(tenant_id,user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
