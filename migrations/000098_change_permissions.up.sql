INSERT INTO permissions(permission_key,description) VALUES
 ('change.read','Odczyt zmian w organizacji'),
 ('change.read_own','Odczyt zgłoszonych i przypisanych zmian oraz kolejki własnych aktywnych grup'),
 ('change.create','Rejestracja zmian'), ('change.manage','Edycja dostępnych zmian'),
 ('change.assign','Przypisywanie dostępnych zmian'), ('change.transition','Kontrolowane przejścia procesu Change'),
 ('change.execute','Wykonywanie wyłącznie komend adaptera procesu Change'),
 ('change.notes','Odczyt i zapis prywatnych notatek dostępnej zmiany'),
 ('change.tasks','Zarządzanie zadaniami wykonawczymi dostępnej zmiany'),
 ('change.schedule_override','Obejście kolizji lub zakazu z uzasadnieniem i audytem'),
 ('change.calendar','Konfiguracja okien serwisowych i okresów zakazu zmian'),
 ('change.template_author','Tworzenie wersjonowanych szablonów Standard Change'),
 ('change.template_approve','Zatwierdzanie i wycofywanie szablonów Standard Change');
