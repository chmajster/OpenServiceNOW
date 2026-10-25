ALTER TABLE cmdb_cis DROP FOREIGN KEY ci_redirect_target, DROP KEY ci_ire_last_seen, DROP KEY ci_redirect, DROP COLUMN redirect_ci_id, DROP COLUMN last_seen_at;
