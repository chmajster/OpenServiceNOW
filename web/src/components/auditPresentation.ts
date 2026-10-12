const labels: Record<string, string> = {
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
