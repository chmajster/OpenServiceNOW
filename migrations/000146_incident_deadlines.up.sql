ALTER TABLE incidents ADD KEY incident_auto_close(state,resolved_at,id);
