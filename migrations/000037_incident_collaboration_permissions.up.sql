INSERT INTO permissions (permission_key,description) VALUES
('incident.communicate','Odpowiedzi dla zgłaszającego i przesyłanie publicznych plików do dostępnego incydentu'),
('incident.notes.read','Odczyt notatek i plików wewnętrznych dostępnego incydentu (wymaga również incident.read)'),
('incident.notes.write','Dodawanie notatek i plików wewnętrznych (wymaga incident.notes.read i incident.read)'),
('incident.attachments.delete','Usuwanie dostępnych plików incydentu');
