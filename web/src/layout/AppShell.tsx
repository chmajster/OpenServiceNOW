import { NavLink, Outlet, useNavigate } from "react-router-dom";
import { useState } from "react";
import { useAuth } from "../auth/AuthContext";

type NavItem = {
  to: string;
  label: string;
  permission?: string;
};

const navigation: NavItem[] = [
  { to: "/", label: "Przegląd" },
  { to: "/tenant", label: "Organizacja", permission: "tenant.read" },
  { to: "/users", label: "Użytkownicy", permission: "admin.users.manage" },
  { to: "/rbac", label: "Role i RBAC", permission: "admin.rbac.manage" },
  { to: "/audit", label: "Audyt", permission: "admin.audit.read" }
];

export function AppShell() {
  const { session, hasPermission, logout } = useAuth();
  const navigate = useNavigate();
  const [loggingOut, setLoggingOut] = useState(false);
  const [logoutError, setLogoutError] = useState<string | null>(null);

  const visibleNavigation = navigation.filter(
    (item) => !item.permission || hasPermission(item.permission)
  );

  async function handleLogout() {
    setLoggingOut(true);
    setLogoutError(null);
    try {
      await logout();
      navigate("/login", { replace: true });
    } catch (cause) {
      setLogoutError(cause instanceof Error ? cause.message : "Wylogowanie nie powiodło się.");
    } finally {
      setLoggingOut(false);
    }
  }

  return (
    <div className="app-shell">
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
            <NavLink
              key={item.to}
              to={item.to}
              end={item.to === "/"}
              className={({ isActive }) => (isActive ? "nav-link nav-link--active" : "nav-link")}
            >
              {item.label}
            </NavLink>
          ))}
        </nav>

        <div className="sidebar-session">
          <span>{session?.tenant.name}</span>
          <strong>{session?.user.display_name}</strong>
          <small>{session?.user.email}</small>
          {logoutError ? <small className="sidebar-error" role="alert">{logoutError}</small> : null}
          <button className="button button--ghost" onClick={() => void handleLogout()} disabled={loggingOut}>
            {loggingOut ? "Wylogowywanie…" : "Wyloguj"}
          </button>
        </div>
      </aside>

      <main className="workspace">
        <header className="topbar">
          <div>
            <strong>{session?.tenant.name}</strong>
            <span className="muted"> / {session?.tenant.slug}</span>
          </div>
          <span className="status-pill">Sesja aktywna</span>
        </header>
        <div className="workspace-content">
          <Outlet />
        </div>
      </main>
    </div>
  );
}
