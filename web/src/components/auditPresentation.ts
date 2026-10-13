const labels: Record<string, string> = {
  change: "Zmiana",
  change_type: "Typ zmiany",
  risk: "Ryzyko",
  justification: "Uzasadnienie zmiany",
  scope: "Zakres zmiany",
  implementation_plan: "Plan wdrożenia",
  test_plan: "Plan testów",
  rollback_plan: "Plan wycofania",
  emergency_reason: "Uzasadnienie pilności",
  review_result: "Opis przeglądu",
  review_outcome: "Wynik przeglądu",
  planned_start: "Planowany początek",
  planned_end: "Planowany koniec",
  actual_start: "Rzeczywisty początek",
  actual_end: "Rzeczywisty koniec",
  time_zone: "Strefa IANA",
  workflow_id: "Proces",
  workflow_version: "Wersja procesu",
  template_id: "Szablon",
  template_version: "Wersja szablonu",
  ci_ids: "Zestaw CI",
  ci_id: "CI",
  max_risk: "Maksymalne ryzyko",
  max_duration_seconds: "Maksymalny czas w sekundach",
  starts_at: "Początek",
  ends_at: "Koniec",
  kind: "Rodzaj",
  config: "Konfiguracja",
  fields: "Pola",
  instructions: "Instrukcje",
  required: "Wymagane",
  result: "Wynik zadania",
  problem: "Problem",
  symptoms: "Objawy",
  analysis: "Wyniki analizy",
  root_cause: "Przyczyna źródłowa",
  workaround: "Obejście",
  solution_plan: "Plan trwałego rozwiązania",
  verification: "Wynik weryfikacji",
  known_error: "Known Error",
  incident: "Incydent",
  id: "Identyfikator",
  number: "Numer",
  title: "Tytuł",
  reporter_id: "Zgłaszający",
  affected_user_id: "Osoba dotknięta problemem",
  category: "Kategoria",
  subcategory: "Podkategoria",
  impact: "Wpływ",
  urgency: "Pilność",
  priority: "Priorytet",
  priority_override: "Nadpisany priorytet",
  priority_reason: "Uzasadnienie priorytetu",
  state: "Stan",
  hold_reason: "Powód wstrzymania",
  reason: "Uzasadnienie",
  resolution_code: "Kod rozwiązania",
  resolution_description: "Opis rozwiązania",
  cancellation_reason: "Powód anulowania",
  assigned_to: "Przypisany użytkownik",
  assignment_group_id: "Grupa przypisania",
  created_at: "Utworzono",
  updated_at: "Zmieniono",
  started_at: "Rozpoczęto obsługę",
  resolved_at: "Rozwiązano",
  closed_at: "Zamknięto",
  cancelled_at: "Anulowano",
  credential_version: "Wersja poświadczeń",
  other_sessions_revoked: "Unieważniono pozostałe sesje",
  revoked_count: "Liczba unieważnionych sesji",
  all_other_sessions: "Wszystkie pozostałe sesje",
  name: "Nazwa",
  description: "Opis",
  permissions: "Uprawnienia",
  version: "Wersja",
  active: "Aktywność",
  email: "E-mail",
  display_name: "Nazwa wyświetlana",
  role_id: "Identyfikator roli",
  user_id: "Identyfikator użytkownika",
  tenant: "Organizacja",
  user: "Użytkownik",
  role: "Rola",
  group: "Grupa",
  session: "Sesja",
  membership_active: "Aktywne członkostwo",
};
const actions: Record<string, string> = {
  "incident.created": "Utworzenie incydentu",
  "incident.updated": "Edycja incydentu",
  "incident.assigned": "Zmiana przypisania incydentu",
  "incident.state.changed": "Zmiana stanu incydentu",
  "incident.reopened": "Ponowne otwarcie incydentu",
  "incident.resolved": "Rozwiązanie incydentu",
  "incident.closed": "Zamknięcie incydentu",
  "incident.cancelled": "Anulowanie incydentu",
  "incident.priority.changed": "Zmiana priorytetu incydentu",
  "account.profile.update": "Zmiana własnego profilu",
  "account.password.change": "Zmiana własnego hasła",
  "account.sessions.revoke": "Unieważnienie własnych sesji",
  "tenant.name.update": "Zmiana nazwy organizacji",
  "rbac.role.create": "Utworzenie roli",
  "rbac.role.permissions.set": "Zmiana uprawnień roli",
  "rbac.group.create": "Utworzenie grupy",
  "rbac.user.role.add": "Przypisanie roli użytkownikowi",
  "rbac.user.role.remove": "Usunięcie roli użytkownika",
  "rbac.group.member.add": "Dodanie członka grupy",
  "rbac.group.member.remove": "Usunięcie członka grupy",
  "rbac.group.role.add": "Przypisanie roli grupie",
  "rbac.group.role.remove": "Usunięcie roli grupy",
  "auth.login": "Logowanie",
  "auth.logout": "Wylogowanie",
  "user.create": "Utworzenie użytkownika",
  "user.local.create": "Utworzenie użytkownika",
  "user.membership.add": "Dodanie członkostwa w organizacji",
  "user.membership.update": "Zmiana członkostwa w organizacji",
  "setup.platform_admin": "Utworzenie administratora podczas instalacji",
  "bootstrap.platform_admin": "Konfiguracja administratora",
};
export function fieldLabel(field: string) {
  return labels[field] ?? field;
}
const permissionLabels: Record<string, string> = {
  "incident.create": "Tworzenie incydentów",
  "incident.read": "Odczyt incydentów organizacji",
  "incident.read_own": "Odczyt własnych zgłoszeń",
  "incident.update": "Edycja i obsługa incydentów",
  "incident.assign": "Przypisywanie incydentów",
  "incident.resolve": "Rozwiązywanie i zamykanie incydentów",
  "incident.priority.override": "Nadpisanie priorytetu z uzasadnieniem",
  "admin.access": "Dostęp do administracji",
  "tenant.read": "Odczyt organizacji",
  "tenant.manage": "Edycja organizacji",
  "admin.users.manage": "Zarządzanie użytkownikami",
  "admin.rbac.manage": "Zarządzanie rolami i grupami",
  "admin.audit.read": "Odczyt audytu",
};
export function permissionLabel(key: string) {
  return permissionLabels[key] ?? key;
}
export function actionLabel(action: string) {
  return actions[action] ?? action;
}
export function displayValue(value: unknown): string {
  return value === undefined || value === null
    ? "—"
    : typeof value === "boolean"
      ? value
        ? "Tak"
        : "Nie"
      : Array.isArray(value)
        ? value.map(displayValue).join(", ") || "Brak"
        : typeof value === "object"
          ? JSON.stringify(value)
          : String(value);
}
function flatten(value: unknown, prefix = ""): Record<string, unknown> {
  if (value === null || value === undefined) return {};
  if (typeof value !== "object" || Array.isArray(value))
    return { [prefix || "Wartość"]: value };
  return Object.fromEntries(
    Object.entries(value).flatMap(([key, item]) =>
      Object.entries(
        item && typeof item === "object" && !Array.isArray(item)
          ? flatten(item, prefix ? `${prefix}.${key}` : key)
          : { [prefix ? `${prefix}.${key}` : key]: item },
      ),
    ),
  );
}
export function auditChanges(before: unknown, after: unknown) {
  const oldFields = flatten(before);
  const newFields = flatten(after);
  return [...new Set([...Object.keys(oldFields), ...Object.keys(newFields)])]
    .sort()
    .filter(
      (field) =>
        JSON.stringify(oldFields[field]) !== JSON.stringify(newFields[field]),
    )
    .map((field) => ({
      field,
      before: oldFields[field],
      after: newFields[field],
    }));
}
