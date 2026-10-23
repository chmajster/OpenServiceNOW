INSERT IGNORE INTO asset_catalog_values(tenant_id,kind,value)
 SELECT DISTINCT tenant_id,'MANUFACTURER',vendor FROM assets WHERE vendor<>'';
