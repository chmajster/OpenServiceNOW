CREATE TABLE record_views (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
 tenant_id BIGINT UNSIGNED NOT NULL,
 owner_user_id BIGINT UNSIGNED NOT NULL,
 group_id BIGINT UNSIGNED NULL,
 domain VARCHAR(32) NOT NULL,
 name VARCHAR(120) NOT NULL,
 configuration JSON NOT NULL,
 version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 PRIMARY KEY(id),
 UNIQUE KEY uq_record_view_owner_name(tenant_id,owner_user_id,domain,name),
 KEY ix_record_views_group(tenant_id,group_id,domain,id),
 CONSTRAINT fk_record_view_tenant FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
 CONSTRAINT fk_record_view_owner FOREIGN KEY(owner_user_id) REFERENCES users(id),
 CONSTRAINT fk_record_view_group FOREIGN KEY(tenant_id,group_id) REFERENCES `groups`(tenant_id,id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
