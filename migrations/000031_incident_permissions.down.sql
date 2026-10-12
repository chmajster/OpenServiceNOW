DELETE FROM permissions WHERE permission_key IN ('incident.create','incident.read','incident.read_own','incident.update','incident.assign','incident.resolve','incident.priority.override');
