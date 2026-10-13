-- DATABASE records deliberately block a lossy downgrade.
ALTER TABLE cmdb_cis DROP CHECK ci_class,
 ADD CONSTRAINT ci_class CHECK(class IN('PHYSICAL_SERVER','VM','NETWORK_DEVICE','APPLICATION','SERVICE')),
 DROP FOREIGN KEY ci_support_group, DROP KEY ci_support,
 DROP CHECK ci_criticality, DROP CHECK ci_lifecycle,
 DROP COLUMN support_group_id, DROP COLUMN criticality, DROP COLUMN lifecycle;
