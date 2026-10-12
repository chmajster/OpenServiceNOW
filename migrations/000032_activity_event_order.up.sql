ALTER TABLE activity_entries ADD KEY idx_activity_business_order (tenant_id, object_type, object_id, source_event_id);
