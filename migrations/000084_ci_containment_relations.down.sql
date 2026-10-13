-- PART_OF records deliberately block a lossy downgrade.
ALTER TABLE cmdb_relations DROP CHECK ci_relation_type, ADD CONSTRAINT ci_relation_type CHECK(relation_type IN('RUNS_ON','HOSTED_ON','DEPENDS_ON','CONNECTS_TO'));
