INSERT INTO permissions (permission_key, description) VALUES
('incident.create', 'Tworzenie incydentów'),
('incident.read', 'Odczyt wszystkich incydentów organizacji'),
('incident.read_own', 'Odczyt incydentów zgłoszonych przez siebie lub dotyczących siebie'),
('incident.update', 'Edycja i obsługa incydentów dostępnych do odczytu'),
('incident.assign', 'Przypisywanie incydentów dostępnych do odczytu'),
('incident.resolve', 'Rozwiązywanie i zamykanie incydentów dostępnych do odczytu'),
('incident.priority.override', 'Ręczne nadpisanie priorytetu z uzasadnieniem');
