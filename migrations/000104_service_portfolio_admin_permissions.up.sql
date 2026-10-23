INSERT IGNORE INTO role_permissions(role_id,permission_id)
SELECT r.id,p.id FROM roles r JOIN permissions p
 ON p.permission_key IN('service_portfolio.read','service_portfolio.read_own','service_portfolio.manage','service_portfolio.offer.read')
WHERE r.name='platform_admin' AND r.system_role=TRUE;
