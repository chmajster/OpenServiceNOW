import {
  Link,
  Outlet,
  ScrollRestoration,
  useLocation,
  useNavigate,
  useSearchParams,
} from "react-router-dom";
import { useEffect, useState } from "react";
import { useAuth } from "../auth/AuthContext";
import { NotificationBadge } from "../notifications/NotificationBadge";
import { WorkspaceProvider, useWorkspace } from "../workspace/WorkspaceContext";
import { GlobalSearch } from "../workspace/GlobalSearch";

type NavItem = {
  to: string;
  label: string;
  section: string;
  permission?: string;
  anyPermissions?: string[];
};
const navigation: NavItem[] = [
  { to: "/", label: "Mój obszar pracy", section: "Praca" },
  {
    to: "/incidents",
    label: "Incydenty",
    section: "Praca",
    anyPermissions: ["incident.read", "incident.read_own"],
  },
  {
    to: "/requests",
    label: "Wnioski",
    section: "Praca",
    anyPermissions: ["request.read_own", "request.fulfill", "request.manage"],
  },
  {
    to: "/fulfillment",
    label: "Zadania i realizacja",
    section: "Praca",
    anyPermissions: ["request.fulfill", "request.manage"],
  },
  {
    to: "/approvals",
    label: "Moje akceptacje",
    section: "Praca",
    permission: "workflow.approve",
  },
  {
    to: "/catalog",
    label: "Katalog usług",
    section: "Usługi",
    permission: "catalog.read",
  },
  {
    to: "/workflow",
    label: "Procesy i akceptacje",
    section: "Administracja",
    anyPermissions: ["workflow.author", "workflow.publish", "workflow.manage"],
  },
  {
    to: "/catalog-admin",
    label: "Administracja katalogiem",
    section: "Administracja",
    permission: "admin.catalog.manage",
  },
  {
    to: "/sla",
    label: "SLA i kalendarze",
    section: "Administracja",
    permission: "admin.sla.manage",
  },
  {
    to: "/notification-admin",
    label: "Dostarczanie powiadomień",
    section: "Administracja",
    permission: "admin.notifications.manage",
  },
  {
    to: "/tenant",
    label: "Organizacja",
    section: "Administracja",
    permission: "tenant.read",
  },
  {
    to: "/users",
    label: "Użytkownicy",
    section: "Administracja",
    permission: "admin.users.manage",
  },
  {
    to: "/rbac?kind=roles",
    label: "Role",
    section: "Administracja",
    permission: "admin.rbac.manage",
  },
  {
    to: "/rbac?kind=groups",
    label: "Grupy",
    section: "Administracja",
    permission: "admin.rbac.manage",
  },
  {
    to: "/audit",
    label: "Audyt",
    section: "Administracja",
    permission: "admin.audit.read",
  },
  {
    to: "/diagnostics",
    label: "Diagnostyka",
    section: "Administracja",
    permission: "admin.rbac.manage",
  },
  { to: "/account", label: "Moje konto", section: "Konto" },
  { to: "/notifications", label: "Powiadomienia", section: "Konto" },
];
export function AppShell() {
  const { session } = useAuth();
  const scope = `${session?.tenant.id}:${session?.user.id}`;
  return (
    <WorkspaceProvider key={scope}>
      <ShellContents />
    </WorkspaceProvider>
  );
}
function ShellContents() {
  const { session, hasPermission, logout } = useAuth();
  const workspace = useWorkspace()!;
  const navigate = useNavigate();
  const location = useLocation();
  const [params] = useSearchParams();
  const kind = params.get("kind") === "groups" ? "groups" : "roles";
  const [collapsed, setCollapsed] = useState(
    () => window.matchMedia("(max-width: 800px)").matches,
  );
  const [contextOpen, setContextOpen] = useState(false);
  const [navQuery, setNavQuery] = useState("");
  const [contextTab, setContextTab] = useState("record");
  const [loggingOut, setLoggingOut] = useState(false);
  const [logoutError, setLogoutError] = useState<string | null>(null);
  const [closing, setClosing] = useState<string | null>(null);
  const visible = navigation.filter((item) =>
    session?.password_change_required
      ? item.to === "/account"
      : (!item.permission || hasPermission(item.permission)) &&
        (!item.anyPermissions || item.anyPermissions.some(hasPermission)),
  );
  const current = visible.find(
    (item) =>
      (item.to.split("?")[0] === location.pathname ||
        (item.to !== "/" && location.pathname.startsWith(item.to + "/"))) &&
      (!item.to.includes("kind=") || item.to.endsWith(kind)),
  );
  const currentRecord = workspace.records.find(
    (r) => r.key === location.pathname,
  );
  const root = currentRecord?.key.split("/").slice(0, 2).join("/");
  const scrollKey = `opensnow-scroll:${session?.tenant.id}:${session?.user.id}`;
  useEffect(
    () => () => {
      sessionStorage.removeItem(scrollKey);
    },
    [scrollKey],
  );
  useEffect(() => {
    if (closing && location.pathname !== closing) {
      workspace.remove(closing);
      setClosing(null);
    }
  }, [closing, location.pathname, workspace]);
  function close(key: string) {
    if (location.pathname !== key) {
      workspace.remove(key);
      return;
    }
    setClosing(key);
    const other = workspace.records.filter((r) => r.key !== key).at(-1);
    navigate(
      other?.url ?? workspace.listURL(key.split("/").slice(0, 2).join("/")),
    );
  }
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
  const toggleContext = (tab: string) => {
    setContextTab(tab);
    setContextOpen(contextTab === tab ? !contextOpen : true);
  };
  return (
    <div
      className={`app-shell operator-shell${collapsed ? " navigation-collapsed" : ""}${contextOpen ? " context-open" : ""}`}
    >
      <a className="skip-link" href="#main-content">
        Przejdź do treści
      </a>
      <header className="topbar global-bar">
        <button
          className="button button--small global-control"
          aria-label={collapsed ? "Rozwiń nawigację" : "Zwiń nawigację"}
          aria-expanded={!collapsed}
          aria-controls="module-navigation"
          onClick={() => setCollapsed(!collapsed)}
        >
          &#9776;
        </button>
        <Link to="/" className="global-brand">
          <span className="brand-mark">OS</span>
          <strong>OpenServiceNOW</strong>
        </Link>
        {!session?.password_change_required && <GlobalSearch />}
        <span
          className="organization-context"
          title={`${session?.tenant.name} / ${session?.tenant.slug}`}
        >
          {session?.tenant.name}
        </span>
        <div className="topbar-actions">
          {!session?.password_change_required && (
            <>
              <button
                className="button button--small global-control"
                aria-expanded={contextOpen && contextTab === "favorites"}
                onClick={() => toggleContext("favorites")}
              >
                Ulubione
              </button>
              <button
                className="button button--small global-control"
                aria-expanded={contextOpen && contextTab === "history"}
                onClick={() => toggleContext("history")}
              >
                Historia
              </button>
              <NotificationBadge />
            </>
          )}
          <details className="user-menu">
            <summary>{session?.user.display_name}</summary>
            <div>
              <Link to="/account">Moje konto</Link>
              <small>{session?.user.email}</small>
              <button
                className="button button--small button--secondary"
                onClick={() => void handleLogout()}
                disabled={loggingOut}
              >
                {loggingOut ? "Wylogowywanie…" : "Wyloguj"}
              </button>
            </div>
          </details>
        </div>
      </header>
      <aside className="sidebar" id="module-navigation" hidden={collapsed}>
        <label className="module-filter">
          Nawigator aplikacji
          <input
            type="search"
            value={navQuery}
            maxLength={100}
            onChange={(e) => setNavQuery(e.target.value)}
            placeholder="Filtruj moduły…"
          />
        </label>
        <nav className="navigation" aria-label="Główna nawigacja">
          {["Praca", "Usługi", "Administracja", "Konto"].map((section) => {
            const items = visible.filter(
              (item) =>
                item.section === section &&
                item.label
                  .toLocaleLowerCase("pl")
                  .includes(navQuery.toLocaleLowerCase("pl")),
            );
            return items.length ? (
              <div className="nav-section" key={section}>
                <h2>{section}</h2>
                {items.map((item) => (
                  <Link
                    key={item.to}
                    onClick={() => {
                      if (window.matchMedia("(max-width: 800px)").matches)
                        setCollapsed(true);
                    }}
                    to={workspace.listURL(item.to)}
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
              </div>
            ) : null;
          })}
          {!visible.some((item) =>
            item.label
              .toLocaleLowerCase("pl")
              .includes(navQuery.toLocaleLowerCase("pl")),
          ) && <p>Brak pasujących modułów.</p>}
        </nav>
      </aside>
      <main className="workspace" id="main-content" tabIndex={-1}>
        <nav className="workspace-tabs" aria-label="Otwarte rekordy">
          <Link
            className={
              !currentRecord ? "is-active workspace-home" : "workspace-home"
            }
            to={session?.password_change_required ? "/account" : "/"}
          >
            Obszar pracy
          </Link>
          {workspace.records.map((r) => (
            <span
              className={
                r.key === location.pathname
                  ? "workspace-tab is-active"
                  : "workspace-tab"
              }
              key={r.key}
            >
              <Link
                to={r.url}
                aria-current={r.key === location.pathname ? "page" : undefined}
                title={r.title}
              >
                {r.number}
              </Link>
              <button
                aria-label={`Zamknij ${r.number}`}
                onClick={() => close(r.key)}
              >
                &#215;
              </button>
            </span>
          ))}
        </nav>
        <div className="workspace-toolbar">
          <nav className="breadcrumbs" aria-label="Ścieżka nawigacji">
            <Link to="/">Mój obszar pracy</Link>
            {location.pathname !== "/" && (
              <>
                <span aria-hidden="true">/</span>
                <Link to={workspace.listURL(current?.to ?? "/")}>
                  {current?.label}
                </Link>
                {currentRecord && (
                  <>
                    <span aria-hidden="true">/</span>
                    <span aria-current="page">{currentRecord.number}</span>
                  </>
                )}
              </>
            )}
          </nav>
          <button
            className="button button--small button--secondary"
            onClick={() => toggleContext("record")}
            aria-expanded={contextOpen && contextTab === "record"}
          >
            Kontekst
          </button>
        </div>
        {logoutError && (
          <p className="notice notice--error" role="alert">
            {logoutError}
          </p>
        )}
        <div className="workspace-content">
          <Outlet />
        </div>
      </main>
      {contextOpen && (
        <aside className="context-panel" aria-label="Kontekst pracy">
          <div className="panel-heading">
            <h2>
              {contextTab === "favorites"
                ? "Ulubione rekordy"
                : contextTab === "history"
                  ? "Ostatnio odwiedzane"
                  : "Kontekst rekordu"}
            </h2>
            <button
              className="button button--small button--secondary"
              onClick={() => setContextOpen(false)}
            >
              Zamknij kontekst
            </button>
          </div>
          <nav className="action-row" aria-label="Panele kontekstu">
            {[
              ["record", "Rekord"],
              ["favorites", "Ulubione"],
              ["history", "Historia"],
            ].map(([value, label]) => (
              <button
                key={value}
                className="button button--small button--secondary"
                aria-pressed={contextTab === value}
                onClick={() => setContextTab(value!)}
              >
                {label}
              </button>
            ))}
          </nav>
          {contextTab === "record" ? (
            currentRecord ? (
              <>
                <p className="eyebrow">{currentRecord.type}</p>
                <h3>{currentRecord.number}</h3>
                <p>{currentRecord.title}</p>
                <dl>
                  <dt>Stan</dt>
                  <dd>{currentRecord.state}</dd>
                  {currentRecord.assignment && (
                    <>
                      <dt>Przypisanie</dt>
                      <dd>{currentRecord.assignment}</dd>
                    </>
                  )}
                </dl>
                <button
                  className="button button--secondary"
                  aria-pressed={workspace.favorites.includes(currentRecord.key)}
                  onClick={() => workspace.favorite(currentRecord.key)}
                >
                  {workspace.favorites.includes(currentRecord.key)
                    ? "Usuń z ulubionych"
                    : "Dodaj do ulubionych"}
                </button>
                {root && (
                  <p>
                    <Link to={workspace.listURL(root)}>Wróć do kolejki</Link>
                  </p>
                )}
                {currentRecord.links?.map((link) => (
                  <p key={link.url}>
                    <Link to={link.url}>{link.label}</Link>
                  </p>
                ))}
              </>
            ) : (
              <p>Otwórz dostępny rekord, aby zobaczyć jego kontekst.</p>
            )
          ) : (
            <ul className="context-records">
              {[...workspace.history]
                .reverse()
                .filter(
                  (r) =>
                    contextTab === "history" ||
                    workspace.favorites.includes(r.key),
                )
                .map((r) => (
                  <li key={r.key}>
                    <Link to={r.url}>
                      <strong>{r.number}</strong>
                      <span>{r.title}</span>
                    </Link>
                  </li>
                ))}
              {(contextTab === "history"
                ? workspace.history.length === 0
                : workspace.favorites.length === 0) && <li>Brak rekordów.</li>}
            </ul>
          )}
          <small className="muted">
            Kontekst sesji jest czyszczony przy wylogowaniu lub zmianie dostępu.
          </small>
        </aside>
      )}
      <ScrollRestoration
        storageKey={scrollKey}
        getKey={(l) => l.pathname + l.search}
      />
    </div>
  );
}
