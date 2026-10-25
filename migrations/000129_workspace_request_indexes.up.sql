ALTER TABLE service_requests ADD KEY idx_service_requests_workspace_created (tenant_id, created_at, id);
