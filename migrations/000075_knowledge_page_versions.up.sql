CREATE TABLE knowledge_page_versions (
 tenant_id BIGINT UNSIGNED NOT NULL,
 space_id BIGINT UNSIGNED NOT NULL,
 page_id BIGINT UNSIGNED NOT NULL,
 version BIGINT UNSIGNED NOT NULL,
 parent_id BIGINT UNSIGNED NULL,
 title VARCHAR(200) NOT NULL,
 body MEDIUMTEXT NOT NULL,
 status VARCHAR(16) NOT NULL,
 review_due_at DATETIME(6) NULL,
 changed_by BIGINT UNSIGNED NOT NULL,
 changed_at DATETIME(6) NOT NULL,
 PRIMARY KEY (tenant_id,page_id,version),
 KEY idx_knowledge_versions_list (tenant_id,page_id,version),
 CONSTRAINT fk_knowledge_versions_page FOREIGN KEY (tenant_id,space_id,page_id) REFERENCES knowledge_pages(tenant_id,space_id,id) ON DELETE CASCADE,
 CONSTRAINT chk_knowledge_versions_status CHECK (status IN('DRAFT','PUBLISHED')),
 CONSTRAINT chk_knowledge_versions_version CHECK (version >= 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
