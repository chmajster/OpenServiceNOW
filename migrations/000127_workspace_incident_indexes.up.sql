ALTER TABLE incidents ADD KEY idx_incidents_workspace_updated (tenant_id, updated_at, id), ADD KEY idx_incidents_workspace_priority (tenant_id, priority, updated_at, id);
