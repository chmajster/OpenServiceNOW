DELETE rp FROM role_permissions rp JOIN permissions p ON p.id=rp.permission_id WHERE p.permission_key IN('cmdb.ire.manage','cmdb.quality.read','cmdb.quality.manage');
