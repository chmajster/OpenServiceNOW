ALTER TABLE metadata_record_values ADD KEY metadata_custom_long_text(tenant_id,record_type,field_key,value_long(100),record_id);
