import {
  Link,
  Outlet,
  useLocation,
  useNavigate,
  useSearchParams,
} from "react-router-dom";
import { useEffect, useState } from "react";
import { useAuth } from "../auth/AuthContext";
import { NotificationBadge } from "../notifications/NotificationBadge";

type NavItem = {
  to: string;
  label: string;
  permission?: string;
  anyPermissions?: string[];
};

const navigation: NavItem[] = [
  {
    to: "/incidents",
    label: "Incydenty",
    anyPermissions: ["incident.read", "incident.read_own"],
  },
  { to: "/account", label: "Moje konto" },
  { to: "/notifications", label: "Powiadomienia" },
  {
    to: "/notification-admin",
    label: "Dostarczanie powiadomień",
    permission: "admin.notifications.manage",
  },
  { to: "/", label: "Przegląd" },
  { to: "/tenant", label: "Organizacja", permission: "tenant.read" },
  { to: "/users", label: "Użytkownicy", permission: "admin.users.manage" },
  { to: "/rbac?kind=roles", label: "Role", permission: "admin.rbac.manage" },
  { to: "/rbac?kind=groups", label: "Grupy", permission: "admin.rbac.manage" },
  { to: "/audit", label: "Audyt", permission: "admin.audit.read" },
  { to: "/diagnostics", label: "Diagnostyka", permission: "admin.rbac.manage" },
];

export function AppShell() {
  const { session, status, hasPermission, logout } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const [params] = useSearchParams();
  const objectID = params.get("id");
  const kind = params.get("kind") === "groups" ? "groups" : "roles";
  useEffect(() => {
    const heading = document.querySelector<HTMLElement>(
      ".workspace-content h1",
    );
    heading?.setAttribute("tabindex", "-1");
    heading?.focus();
  }, [location.pathname, objectID, kind]);
  const current = navigation.find(
    (item) =>
      (item.to.split("?")[0] === location.pathname ||
        (item.to === "/incidents" &&
          location.pathname.startsWith("/incidents/"))) &&
      (!item.to.includes("kind=") || item.to.endsWith(kind)),
  );
  const [loggingOut, setLoggingOut] = useState(false);
  const [logoutError, setLogoutError] = useState<string | null>(null);

  const visibleNavigation = navigation.filter((item) =>
    session?.password_change_required
      ? item.to === "/account"
      : (!item.permission || hasPermission(item.permission)) &&
        (!item.anyPermissions || item.anyPermissions.some(hasPermission)),
  );

  async function handleLogout() {
    setLoggingOut(true);
    setLogoutError(null);
    try {
      await logout();
      navigate("/login", { replace: true });
    } catch (cause) {
      setLogoutError(
        cause instanceof Error
          ? cause.message
          : "Wylogowanie nie powiodło się.",
      );
    } finally {
      setLoggingOut(false);
    }
  }

  return (
    <div className="app-shell">
      <a className="skip-link" href="#main-content">
        Przejdź do treści
      </a>
      <aside className="sidebar">
        <div className="brand">
          <span className="brand-mark">OS</span>
          <div>
            <strong>OpenServiceNOW</strong>
            <small>IT Service Platform</small>
          </div>
        </div>

        <nav className="navigation" aria-label="Główna nawigacja">
          {visibleNavigation.map((item) => (
            <Link
              key={item.to}
              to={item.to}
              aria-current={current?.to === item.to ? "page" : undefined}
              className={
                current?.to === item.to
                  ? "nav-link nav-link--active"
                  : "nav-link"
              }
            >
              {item.label}
            </Link>
          ))}
        </nav>

        <div className="sidebar-session">
          <span>{session?.tenant.name}</span>
          <strong>{session?.user.display_name}</strong>
          <small>{session?.user.email}</small>
          {logoutError ? (
            <small className="sidebar-error" role="alert">
              {logoutError}
            </small>
          ) : null}
          <button
            className="button button--ghost"
            onClick={() => void handleLogout()}
            disabled={loggingOut}
          >
            {loggingOut ? "Wylogowywanie…" : "Wyloguj"}
          </button>
        </div>
      </aside>

      <main className="workspace" id="main-content" tabIndex={-1}>
        <header className="topbar">
          <div>
            <strong>{session?.tenant.name}</strong>
            <span className="muted"> / {session?.tenant.slug}</span>
          </div>
          <div className="topbar-actions">
            {status === "authenticated" &&
            session &&
            !session.password_change_required ? (
              <NotificationBadge />
            ) : null}
            {status === "authenticated" && session ? (
              <span className="status-pill">Zalogowano</span>
            ) : null}
            {logoutError ? (
              <span className="topbar-error" role="alert">
                {logoutError}
              </span>
            ) : null}
            <button
              className="button button--small button--secondary topbar-logout"
              onClick={() => void handleLogout()}
              disabled={loggingOut}
            >
              {loggingOut ? "Wylogowywanie…" : "Wyloguj"}
            </button>
          </div>
        </header>
        <div className="workspace-content">
          <nav className="breadcrumbs" aria-label="Ścieżka nawigacji">
            <Link to={session?.password_change_required ? "/account" : "/"}>
              {session?.password_change_required ? "Moje konto" : "Przegląd"}
            </Link>
            {location.pathname !== "/" ? (
              location.pathname.startsWith("/incidents") ? (
                <>
                  <span aria-hidden="true">/</span>
                  {location.pathname === "/incidents" ? (
                    <span aria-current="page">Incydenty</span>
                  ) : (
                    <>
                      <Link to="/incidents">Incydenty</Link>
                      <span aria-hidden="true">/</span>
                      <span aria-current="page">
                        {location.pathname.endsWith("/new")
                          ? "Nowy incydent"
                          : `#${location.pathname.split("/")[2]}`}
                      </span>
                    </>
                  )}
                </>
              ) : (
                <>
                  <span aria-hidden="true">/</span>
                  <span>
                    {location.pathname === "/account"
                      ? "Konto"
                      : "Administracja"}
                  </span>
                  <span aria-hidden="true">/</span>
                  {objectID ? (
                    <>
                      <Link to={`/rbac?kind=${kind}`}>{current?.label}</Link>
                      <span aria-hidden="true">/</span>
                      <span aria-current="page">
                        {params.get("create") === "1"
                          ? "Tworzenie"
                          : `#${objectID}`}
                      </span>
                    </>
                  ) : (
                    <span aria-current="page">{current?.label}</span>
                  )}
                </>
              )
            ) : null}
          </nav>
          <Outlet />
        </div>
      </main>
    </div>
  );
}
