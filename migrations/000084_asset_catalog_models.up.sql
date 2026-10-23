INSERT IGNORE INTO asset_catalog_values(tenant_id,kind,value,manufacturer)
 SELECT DISTINCT tenant_id,'MODEL',model,vendor FROM assets WHERE model<>'';
