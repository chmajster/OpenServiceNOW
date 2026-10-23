CREATE TABLE service_portfolio_offer_audience_groups (
 tenant_id BIGINT UNSIGNED NOT NULL,
 offer_id BIGINT UNSIGNED NOT NULL,
 group_id BIGINT UNSIGNED NOT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 PRIMARY KEY(tenant_id,offer_id,group_id),
 KEY service_portfolio_offer_audience_group(tenant_id,group_id,offer_id),
 CONSTRAINT service_portfolio_offer_audience_offer FOREIGN KEY(tenant_id,offer_id) REFERENCES service_portfolio_offers(tenant_id,id) ON DELETE CASCADE,
 CONSTRAINT service_portfolio_offer_audience_group_fk FOREIGN KEY(tenant_id,group_id) REFERENCES `groups`(tenant_id,id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
