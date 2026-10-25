ALTER TABLE fulfillment_tasks ADD KEY idx_fulfillment_tasks_workspace_updated (tenant_id, updated_at, id), ADD KEY idx_fulfillment_tasks_workspace_state_updated (tenant_id, state, updated_at, id);
