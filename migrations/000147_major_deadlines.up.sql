ALTER TABLE incident_major_cases ADD KEY major_deadline_scan(status,next_update_at,tenant_id,incident_id);
