ALTER TABLE cmdb_cis
 DROP CHECK ci_class,
 ADD CONSTRAINT ci_class CHECK(class IN('PHYSICAL_SERVER','VM','NETWORK_DEVICE','APPLICATION','DATABASE','SERVICE')),
 ADD support_group_id BIGINT UNSIGNED NULL,
 ADD criticality VARCHAR(20) NOT NULL DEFAULT 'NORMAL',
 ADD lifecycle VARCHAR(20) NOT NULL DEFAULT 'DESIGN',
 ADD CONSTRAINT ci_support_group FOREIGN KEY(tenant_id,support_group_id) REFERENCES `groups`(tenant_id,id),
 ADD CONSTRAINT ci_criticality CHECK(criticality IN('CRITICAL','HIGH','NORMAL','LOW')),
 ADD CONSTRAINT ci_lifecycle CHECK(lifecycle IN('DESIGN','BUILD','OPERATE','RETIRE')),
 ADD KEY ci_support(tenant_id,support_group_id,id);
