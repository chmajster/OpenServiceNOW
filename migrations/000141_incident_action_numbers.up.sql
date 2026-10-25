UPDATE incident_major_actions SET number=CONCAT('ACTL',LPAD(CAST(id AS CHAR),7,'0')) WHERE number='';
