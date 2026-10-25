import {
  Link,
  Outlet,
  useLocation,
  useNavigate,
  useSearchParams,
} from "react-router-dom";
import { useEffect, useState } from "react";
import { api, jsonBody } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { NotificationBadge } from "../notifications/NotificationBadge";
import { setCMDBLocale, useCMDBLocale } from "../cmdb/messages";
import type { Locale } from "../metadata/contracts";

type LocalePreferences = { locale?: Locale; time_zone?: string; version: string; organization_name: string; brand_color: string; logo_url?: string; default_locale: Locale; default_time_zone: string; default_date_format: string; default_number_format: string; default_currency: string };

function brandTextColor(color: string) {
  const match = /^#([0-9a-f]{6})$/i.exec(color);
  if (!match) return "#1e3538";
  const channels = [0, 2, 4].map((index) => parseInt(match[1]!.slice(index, index + 2), 16) / 255);
  const luminance = channels.map((value) => value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4)
    .reduce((sum, value, index) => sum + value * [0.2126, 0.7152, 0.0722][index]!, 0);
  return luminance > 0.179 ? "#111827" : "#ffffff";
}

type NavIconName =
  | "home"
  | "check"
  | "workflow"
  | "grid"
  | "clipboard"
  | "inbox"
  | "alert"
  | "user"
  | "bell"
  | "clock"
  | "send"
  | "building"
  | "users"
  | "key"
  | "list"
  | "activity";

type NavItem = {
  to: string;
  label: string;
  icon: NavIconName;
  permission?: string;
  anyPermissions?: string[];
  anyPermissionSets?: string[][];
};

type NavGroup = {
  label: string;
  items: NavItem[];
};

const navigationGroups: NavGroup[] = [
  {
    label: "Moja praca",
    items: [
      { to: "/", label: "Przegląd", icon: "home" },
      {
        to: "/workspace",
        label: "Workspace operatora",
        icon: "inbox",
        anyPermissions: ["incident.read", "incident.read_own", "request.fulfill", "request.manage"],
      },
      {
        to: "/approvals",
        label: "Moje akceptacje",
        icon: "check",
        permission: "workflow.approve",
      },
      {
        to: "/workflow",
        label: "Procesy i akceptacje",
        icon: "workflow",
        anyPermissions: ["workflow.author", "workflow.publish", "workflow.manage"],
      },
      {
        to: "/catalog",
        label: "Katalog usług",
        icon: "grid",
        permission: "catalog.read",
      },
      {
        to: "/requests",
        label: "Moje wnioski",
        icon: "clipboard",
        anyPermissions: ["request.read_own", "request.fulfill", "request.manage"],
      },
      {
        to: "/fulfillment",
        label: "Kolejka realizacji",
        icon: "inbox",
        anyPermissions: ["request.fulfill", "request.manage"],
      },
      {
        to: "/incidents",
        label: "Incydenty",
        icon: "alert",
        anyPermissions: ["incident.read", "incident.read_own"],
      },
      {
        to: "/assets",
        label: "Zasoby IT (ITAM)",
        icon: "grid",
        anyPermissions: ["assets.read", "assets.read_own"],
      },
      {
        to: "/cmdb",
        label: "Elementy konfiguracji (CMDB)",
        icon: "grid",
        anyPermissions: ["cmdb.read", "cmdb.read_own"],
      },
      {
        to: "/service-portfolio",
        label: "Portfel usług (CSDM)",
        icon: "activity",
        anyPermissions: ["service_portfolio.read", "service_portfolio.read_own", "service_portfolio.manage"],
      },
      {
        to: "/service-portfolio/offers",
        label: "Oferty usług",
        icon: "grid",
        anyPermissions: ["service_portfolio.offer.read", "catalog.read", "service_portfolio.read", "service_portfolio.manage"],
      },
    ],
  },
  {
    label: "Konto",
    items: [
      { to: "/account", label: "Moje konto", icon: "user" },
      { to: "/notifications", label: "Powiadomienia", icon: "bell" },
    ],
  },
  {
    label: "Administracja",
    items: [
      {
        to: "/cmdb/imports",
        label: "Importy CI i Asset",
        icon: "list",
        permission: "cmdb.import",
        anyPermissionSets: [
          ["cmdb.manage", "cmdb.read"],
          ["assets.manage", "assets.read"],
        ],
      },
      {
        to: "/cmdb/ire",
        label: "Uzgadnianie CI i jakość CMDB (IRE)",
        icon: "list",
        anyPermissions: ["cmdb.ire.manage", "cmdb.quality.read", "cmdb.quality.manage"],
      },
      {
        to: "/catalog-admin",
        label: "Administracja katalogiem",
        icon: "grid",
        permission: "admin.catalog.manage",
      },
      {
        to: "/sla",
        label: "SLA i kalendarze",
        icon: "clock",
        permission: "admin.sla.manage",
      },
      {
        to: "/notification-admin",
        label: "Dostarczanie powiadomień",
        icon: "send",
        permission: "admin.notifications.manage",
      },
      {
        to: "/metadata",
        label: "Słowniki i formularze",
        icon: "list",
        permission: "admin.metadata.manage",
      },
      {
        to: "/tenant",
        label: "Organizacja",
        icon: "building",
        permission: "tenant.read",
      },
      {
        to: "/users",
        label: "Użytkownicy",
        icon: "users",
        permission: "admin.users.manage",
      },
      {
        to: "/rbac?kind=roles",
        label: "Role",
        icon: "key",
        permission: "admin.rbac.manage",
      },
      {
        to: "/rbac?kind=groups",
        label: "Grupy",
        icon: "users",
        permission: "admin.rbac.manage",
      },
      {
        to: "/audit",
        label: "Audyt",
        icon: "list",
        permission: "admin.audit.read",
      },
      {
        to: "/diagnostics",
        label: "Diagnostyka",
        icon: "activity",
        permission: "admin.rbac.manage",
      },
    ],
  },
];

const navigation = navigationGroups.flatMap((group) => group.items);

function NavIcon({ name }: { name: NavIconName }) {
  const common = {
    "aria-hidden": true as const,
    focusable: false as const,
    fill: "none",
    stroke: "currentColor",
    strokeLinecap: "round" as const,
    strokeLinejoin: "round" as const,
    strokeWidth: 1.7,
    viewBox: "0 0 24 24",
  };

  switch (name) {
    case "home":
      return (
        <svg {...common}>
          <path d="m3 10 9-7 9 7" />
          <path d="M5.5 9.5V21h13V9.5M9 21v-7h6v7" />
        </svg>
      );
    case "check":
      return (
        <svg {...common}>
          <circle cx="12" cy="12" r="9" />
          <path d="m8.5 12.3 2.2 2.2 4.8-5" />
        </svg>
      );
    case "workflow":
      return (
        <svg {...common}>
          <rect x="3" y="4" width="8" height="6" rx="1" />
          <rect x="13" y="14" width="8" height="6" rx="1" />
          <path d="M11 7h2a2 2 0 0 1 2 2v5" />
        </svg>
      );
    case "grid":
      return (
        <svg {...common}>
          <rect x="3.5" y="3.5" width="7" height="7" rx="1" />
          <rect x="13.5" y="3.5" width="7" height="7" rx="1" />
          <rect x="3.5" y="13.5" width="7" height="7" rx="1" />
          <rect x="13.5" y="13.5" width="7" height="7" rx="1" />
        </svg>
      );
    case "clipboard":
      return (
        <svg {...common}>
          <rect x="5" y="4.5" width="14" height="17" rx="1.5" />
          <path d="M9 4.5V3h6v1.5M8.5 10h7M8.5 14h7M8.5 18h4.5" />
        </svg>
      );
    case "inbox":
      return (
        <svg {...common}>
          <path d="M4 4h16v13h-4l-2 3h-4l-2-3H4z" />
          <path d="M4 13h4l1.5 2h5L16 13h4" />
        </svg>
      );
    case "alert":
      return (
        <svg {...common}>
          <path d="m12 3 9 17H3z" />
          <path d="M12 9v5M12 17.5h.01" />
        </svg>
      );
    case "user":
      return (
        <svg {...common}>
          <circle cx="12" cy="8" r="3.5" />
          <path d="M4.5 21a7.5 7.5 0 0 1 15 0" />
        </svg>
      );
    case "bell":
      return (
        <svg {...common}>
          <path d="M18 9a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4" />
        </svg>
      );
    case "clock":
      return (
        <svg {...common}>
          <circle cx="12" cy="12" r="9" />
          <path d="M12 7v5l3.5 2" />
        </svg>
      );
    case "send":
      return (
        <svg {...common}>
          <path d="m21 3-7.5 18-3.8-7.7L2 9.5z" />
          <path d="M9.7 13.3 21 3" />
        </svg>
      );
    case "building":
      return (
        <svg {...common}>
          <path d="M4 21V5l8-2v18M12 9h8v12M2 21h20" />
          <path d="M7 7h2M7 11h2M7 15h2M15 12h2M15 16h2" />
        </svg>
      );
    case "users":
      return (
        <svg {...common}>
          <circle cx="9" cy="8" r="3" />
          <path d="M3 20a6 6 0 0 1 12 0M16 5.5a3 3 0 0 1 0 5.8M17 14a5 5 0 0 1 4 5" />
        </svg>
      );
    case "key":
      return (
        <svg {...common}>
          <circle cx="8" cy="15" r="5" />
          <path d="m11.5 11.5 9-9M16 7l2 2M13 10l2 2" />
        </svg>
      );
    case "list":
      return (
        <svg {...common}>
          <path d="M9 6h11M9 12h11M9 18h11" />
          <circle cx="4.5" cy="6" r=".7" />
          <circle cx="4.5" cy="12" r=".7" />
          <circle cx="4.5" cy="18" r=".7" />
        </svg>
      );
    case "activity":
      return (
        <svg {...common}>
          <path d="M3 12h4l2.2-7 4.6 14 2.2-7H21" />
        </svg>
      );
  }
}

export function AppShell() {
  const { session, status, hasPermission, logout } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const [params] = useSearchParams();
  const objectID = params.get("id");
  const kind = params.get("kind") === "groups" ? "groups" : "roles";
  const [loggingOut, setLoggingOut] = useState(false);
  const [logoutError, setLogoutError] = useState<string | null>(null);
  const [navigationFilter, setNavigationFilter] = useState("");
  const [globalSearch, setGlobalSearch] = useState("");
  const [mobileNavOpen, setMobileNavOpen] = useState(false);
  const [localePreferences, setLocalePreferences] = useState<LocalePreferences>();
  const [logoFailed, setLogoFailed] = useState(false);
  const [savingLocale, setSavingLocale] = useState(false);
  const [localeError, setLocaleError] = useState<string>();
  const cmdbLocale = useCMDBLocale();

  useEffect(() => {
    if (location.pathname === "/search")
      setGlobalSearch(params.get("q") ?? "");
  }, [location.pathname, params]);

  useEffect(() => {
    const heading = document.querySelector<HTMLElement>(
      ".workspace-content h1",
    );
    heading?.setAttribute("tabindex", "-1");
    heading?.focus();
  }, [location.pathname, objectID, kind]);

  useEffect(() => {
    setMobileNavOpen(false);
  }, [location.pathname, location.search]);

  useEffect(() => {
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setMobileNavOpen(false);
    };
    window.addEventListener("keydown", closeOnEscape);
    return () => window.removeEventListener("keydown", closeOnEscape);
  }, []);

  const current = [...navigation]
    .sort((left, right) => right.to.split("?")[0]!.length - left.to.split("?")[0]!.length)
    .find((item) => {
    const path = item.to.split("?")[0] ?? item.to;
    const nestedRoute = [
      "/incidents",
      "/catalog",
      "/requests",
      "/fulfillment",
      "/approvals",
      "/assets",
      "/cmdb",
      "/cmdb/imports",
    ].includes(path);
    const matchesPath =
      path === location.pathname ||
      (nestedRoute && location.pathname.startsWith(path + "/"));
    return matchesPath && (!item.to.includes("kind=") || item.to.endsWith(kind));
    });
  const isAssetRoute =
    location.pathname === "/assets" || location.pathname.startsWith("/assets/");
  const isCMDBRoute =
    location.pathname === "/cmdb" || location.pathname.startsWith("/cmdb/");
  const isCMDBImportRoute =
    location.pathname === "/cmdb/imports" || location.pathname.startsWith("/cmdb/imports/");
  useEffect(() => {
    document.documentElement.lang = cmdbLocale;
  }, [cmdbLocale]);
  useEffect(() => {
    if (!session) {
      setLocalePreferences(undefined);
      delete document.documentElement.dataset.timeZone;
      delete document.documentElement.dataset.dateFormat;
      delete document.documentElement.dataset.numberFormat;
      document.documentElement.style.removeProperty("--organization-brand-color");
      document.documentElement.style.removeProperty("--organization-brand-contrast");
      return;
    }
    let requestController: AbortController | undefined;
    const loadPreferences = () => {
      requestController?.abort();
      const controller = new AbortController();
      requestController = controller;
      setLocaleError(undefined);
      void api<LocalePreferences>("/api/v1/metadata/preferences", { signal: controller.signal })
        .then((preferences) => {
          if (controller.signal.aborted) return;
          setLocalePreferences(preferences);
          setCMDBLocale(preferences.locale || preferences.default_locale);
          document.documentElement.dataset.timeZone = preferences.time_zone || preferences.default_time_zone;
          document.documentElement.dataset.dateFormat = preferences.default_date_format;
          document.documentElement.dataset.numberFormat = preferences.default_number_format;
          const brandColor = /^#[0-9a-f]{6}$/i.test(preferences.brand_color) ? preferences.brand_color : "#0f766e";
          document.documentElement.style.setProperty("--organization-brand-color", brandColor);
          document.documentElement.style.setProperty("--organization-brand-contrast", brandTextColor(brandColor));
          setLogoFailed(false);
        })
        .catch((cause) => {
          if (!controller.signal.aborted) setLocaleError(cause instanceof Error ? cause.message : String(cause));
        });
    };
    window.addEventListener("opensnow:metadata-published", loadPreferences);
    loadPreferences();
    return () => {
      window.removeEventListener("opensnow:metadata-published", loadPreferences);
      requestController?.abort();
    };
  }, [session?.tenant.id, session?.user.id]);
  const isCollectionRoute = [
    "/incidents",
    "/workspace",
    "/catalog",
    "/requests",
    "/fulfillment",
  ].some(
    (root) =>
      location.pathname === root || location.pathname.startsWith(root + "/"),
  ) || isAssetRoute || isCMDBRoute || location.pathname === "/service-portfolio" || location.pathname.startsWith("/service-portfolio/");
  const isPortfolioRoute = location.pathname === "/service-portfolio" || location.pathname.startsWith("/service-portfolio/");

  const query = navigationFilter.trim().toLocaleLowerCase("pl-PL");
  const visibleGroups = navigationGroups
    .map((group) => ({
      ...group,
      items: group.items.filter((item) => {
        const permitted = session?.password_change_required
          ? item.to === "/account"
          : (!item.permission || hasPermission(item.permission)) &&
            (!item.anyPermissions || item.anyPermissions.some(hasPermission)) &&
            (!item.anyPermissionSets ||
              item.anyPermissionSets.some((set) => set.every(hasPermission)));
        return permitted && item.label.toLocaleLowerCase("pl-PL").includes(query);
      }),
    }))
    .filter((group) => group.items.length > 0);

  async function handleLogout() {
    setLoggingOut(true);
    setLogoutError(null);
    try {
      await logout();
      navigate("/login", { replace: true });
    } catch (cause) {
      setLogoutError(
        cause instanceof Error ? cause.message : "Wylogowanie nie powiodło się.",
      );
    } finally {
      setLoggingOut(false);
    }
  }

  async function changeLocale() {
    const next: Locale = cmdbLocale === "en" ? "pl" : "en";
    setCMDBLocale(next);
    setLocaleError(undefined);
    if (!session || !localePreferences || savingLocale) return;
    setSavingLocale(true);
    try {
      const updated = await api<LocalePreferences>("/api/v1/metadata/preferences", {
        method: "PUT",
        body: jsonBody({ version: localePreferences.version, locale: next, time_zone: localePreferences.time_zone ?? "" }),
      });
      setLocalePreferences(updated);
      setCMDBLocale(updated.locale || updated.default_locale);
    } catch (cause) {
      setLocaleError(cause instanceof Error ? cause.message : "Nie udało się zapisać preferencji języka.");
    } finally { setSavingLocale(false); }
  }

  const displayName = session?.user.display_name ?? "Użytkownik";
  const initials = displayName
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0])
    .join("")
    .toLocaleUpperCase("pl-PL");

  return (
    <div className="app-shell">
      <a className="skip-link" href="#main-content">
        Przejdź do treści
      </a>

      <header className="topbar">
        <div className="topbar-brand">
          <button
            className="mobile-menu-button"
            type="button"
            aria-label={mobileNavOpen ? "Zamknij nawigację" : "Otwórz nawigację"}
            aria-expanded={mobileNavOpen}
            aria-controls="app-navigation"
            onClick={() => setMobileNavOpen((open) => !open)}
          >
            <svg
              aria-hidden="true"
              focusable="false"
              fill="none"
              stroke="currentColor"
              strokeLinecap="round"
              strokeWidth="2"
              viewBox="0 0 24 24"
            >
              {mobileNavOpen ? (
                <path d="m6 6 12 12M18 6 6 18" />
              ) : (
                <path d="M4 6h16M4 12h16M4 18h16" />
              )}
            </svg>
          </button>

          <div className="brand">
            <span className="brand-mark">{localePreferences?.logo_url && !logoFailed ? <img src={localePreferences.logo_url} alt="" onError={() => setLogoFailed(true)} /> : "OS"}</span>
            <span className="brand-copy">
              <strong>{localePreferences?.organization_name || "OpenServiceNOW"}</strong>
              <small>IT Service Platform</small>
            </span>
          </div>
          <span className="topbar-divider" aria-hidden="true" />
          <span className="topbar-product">Service Management</span>
        </div>

        {!session?.password_change_required ? (
          <form
            className="topbar-global-search"
            role="search"
            aria-label="Wyszukiwanie globalne"
            onSubmit={(event) => {
              event.preventDefault();
              const query = globalSearch.trim();
              if (query) navigate(`/search?q=${encodeURIComponent(query)}`);
            }}
          >
            <svg
              aria-hidden="true"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
            >
              <circle cx="10.8" cy="10.8" r="6.8" />
              <path d="m16 16 5 5" />
            </svg>
            <input
              type="search"
              aria-label="Szukaj globalnie"
              placeholder="Szukaj INC, grup, użytkowników, assetów, zadań…"
              value={globalSearch}
              maxLength={160}
              onChange={(event) => setGlobalSearch(event.target.value)}
            />
            <kbd>Enter</kbd>
          </form>
        ) : null}

        <div className="topbar-actions">
          {session && !session.password_change_required && (
            <button
              className="button button--small button--secondary"
              type="button"
              aria-label={cmdbLocale === "en" ? "Change application language to Polish" : "Zmień język aplikacji na angielski"}
              disabled={savingLocale || !localePreferences}
              title={localeError}
              onClick={() => void changeLocale()}
            >
              {cmdbLocale === "en" ? "Polski" : "English"}
            </button>
          )}
          {session ? (
            <div className="tenant-context">
              <strong>{session.tenant.name}</strong>
              <span>{session.tenant.slug}</span>
            </div>
          ) : null}
          {status === "authenticated" &&
          session &&
          !session.password_change_required ? (
            <NotificationBadge />
          ) : null}
          {session ? (
            <Link
              className="topbar-user"
              to="/account"
              aria-label={"Konto: " + displayName}
            >
              <span className="user-avatar" aria-hidden="true">
                {initials || "OS"}
              </span>
              <span className="topbar-user-copy">
                <strong>{displayName}</strong>
                <small>Konto</small>
              </span>
            </Link>
          ) : null}
          {logoutError ? (
            <span className="topbar-error" role="alert">
              {logoutError}
            </span>
          ) : null}
          <button
            className="button button--small button--topbar topbar-logout"
            onClick={() => void handleLogout()}
            disabled={loggingOut}
          >
            {loggingOut ? "Wylogowywanie…" : "Wyloguj"}
          </button>
        </div>
      </header>

      {mobileNavOpen ? (
        <button
          className="sidebar-backdrop"
          type="button"
          tabIndex={-1}
          aria-label="Zamknij nawigację"
          onClick={() => setMobileNavOpen(false)}
        />
      ) : null}

      <aside
        className={"sidebar" + (mobileNavOpen ? " sidebar--open" : "")}
        id="app-navigation"
        aria-label="Nawigacja aplikacji"
      >
        <div className="sidebar-app-title">Service Management</div>
        <label className="navigation-search">
          <span className="sr-only">Filtruj nawigację</span>
          <svg
            aria-hidden="true"
            focusable="false"
            fill="none"
            stroke="currentColor"
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth="1.8"
            viewBox="0 0 24 24"
          >
            <circle cx="10.8" cy="10.8" r="6.8" />
            <path d="m16 16 5 5" />
          </svg>
          <input
            type="search"
            value={navigationFilter}
            onChange={(event) => setNavigationFilter(event.target.value)}
            placeholder="Filtruj nawigator"
            autoComplete="off"
          />
        </label>

        <nav
          className="navigation"
          id="navigation-links"
          aria-label="Główna nawigacja"
        >
          {visibleGroups.map((group) => (
            <section className="navigation-group" key={group.label}>
              <h2 className="navigation-group-title">{group.label}</h2>
              {group.items.map((item) => {
                const active = current?.to === item.to;
                return (
                  <Link
                    key={item.to}
                    to={item.to}
                    aria-current={active ? "page" : undefined}
                    className={active ? "nav-link nav-link--active" : "nav-link"}
                    onClick={() => setMobileNavOpen(false)}
                  >
                    <NavIcon name={item.icon} />
                    <span>{item.label}</span>
                  </Link>
                );
              })}
            </section>
          ))}
          {visibleGroups.length === 0 ? (
            <p className="nav-empty" role="status">
              Nie znaleziono modułów.
            </p>
          ) : null}
        </nav>
      </aside>

      <main className="workspace" id="main-content" tabIndex={-1}>
        <div className="workspace-content">
          <nav className="breadcrumbs" aria-label="Ścieżka nawigacji">
            <Link to={session?.password_change_required ? "/account" : "/"}>
              {session?.password_change_required ? "Moje konto" : "Przegląd"}
            </Link>
          {location.pathname !== "/" ? (
              location.pathname === "/search" ? (
                <>
                  <span aria-hidden="true">/</span>
                  <span aria-current="page">Wyszukiwanie globalne</span>
                </>
              ) : isCollectionRoute ? (
                <>
                  <span aria-hidden="true">/</span>
                  {isPortfolioRoute ? (
                    location.pathname === "/service-portfolio" || location.pathname === "/service-portfolio/offers" || location.pathname === "/service-portfolio/quality" || location.pathname === "/service-portfolio/classification" || location.pathname === "/service-portfolio/impact/ci" ? (
                      <span aria-current="page">{location.pathname.endsWith("/offers") ? "Oferty usług" : location.pathname.endsWith("/quality") ? "Jakość danych" : location.pathname.endsWith("/classification") ? "Klasyfikacja CI" : current?.label ?? "Portfel usług (CSDM)"}</span>
                    ) : <><Link to="/service-portfolio">{current?.label ?? "Portfel usług (CSDM)"}</Link><span aria-hidden="true">/</span><span aria-current="page">#{location.pathname.split("/").at(-1)}</span></>
                  ) : location.pathname.split("/").length === 2 || location.pathname === "/cmdb/imports" || location.pathname === "/cmdb/ire" ? (
                    <span aria-current="page">{current?.label}</span>
                  ) : (
                    <>
                      <Link to={current?.to ?? "/"}>{current?.label}</Link>
                      <span aria-hidden="true">/</span>
                      <span aria-current="page">
                        {location.pathname.endsWith("/new")
                          ? isAssetRoute
                            ? "Nowy zasób"
                            : isCMDBRoute
                              ? "Nowy CI"
                              : "Nowy incydent"
                          : "#" + (isCMDBImportRoute ? location.pathname.split("/")[3] : location.pathname.split("/")[2])}
                      </span>
                    </>
                  )}
                </>
              ) : (
                <>
                  <span aria-hidden="true">/</span>
                  <span>
                    {location.pathname === "/account" ||
                    location.pathname === "/notifications"
                      ? "Konto"
                      : location.pathname === "/workflow" ||
                          location.pathname === "/approvals"
                        ? "Moja praca"
                        : "Administracja"}
                  </span>
                  <span aria-hidden="true">/</span>
                  {objectID ? (
                    <>
                      <Link
                        to={
                          location.pathname === "/catalog-admin"
                            ? "/catalog-admin"
                            : location.pathname === "/sla"
                              ? "/sla?kind=" + (params.get("kind") ?? "policy")
                              : "/rbac?kind=" + kind
                        }
                      >
                        {current?.label}
                      </Link>
                      <span aria-hidden="true">/</span>
                      <span aria-current="page">
                        {params.get("create") === "1" ||
                        (location.pathname === "/sla" && objectID === "new")
                          ? "Tworzenie"
                          : "#" + objectID}
                      </span>
                    </>
                  ) : (
                    <span aria-current="page">{current?.label ?? "Administracja"}</span>
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
