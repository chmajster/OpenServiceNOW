ALTER TABLE cmdb_cis
 ADD KEY ci_name_search(tenant_id,name,id),
 ADD KEY ci_status_filter(tenant_id,status,id),
 ADD KEY ci_environment_filter(tenant_id,environment,id);
