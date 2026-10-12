import { Link } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
export function OverviewPage() {
  const { session, hasPermission } = useAuth();
  const links: [string, string, string][] = [
    ["/tenant", "Organizacja", "tenant.read"],
    ["/users", "Użytkownicy", "admin.users.manage"],
    ["/rbac?kind=roles", "Role", "admin.rbac.manage"],
    ["/rbac?kind=groups", "Grupy", "admin.rbac.manage"],
    ["/audit", "Audyt", "admin.audit.read"],
  ];
  return (
    <section className="page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">OpenServiceNOW</p>
          <h1>Przegląd</h1>
          <p className="muted">{session?.tenant.name}</p>
        </div>
      </div>
      <div className="panel">
        <h2>Twoje konto</h2>
        <dl className="detail-grid">
          <div>
            <dt>Użytkownik</dt>
            <dd>{session?.user.display_name}</dd>
          </div>
          <div>
            <dt>E-mail</dt>
            <dd>{session?.user.email}</dd>
          </div>
          <div>
            <dt>Organizacja</dt>
            <dd>{session?.tenant.name}</dd>
          </div>
        </dl>
      </div>
      <div className="panel">
        <h2>Obsługa zgłoszeń</h2>
        {hasPermission("incident.read") ||
        hasPermission("incident.read_own") ? (
          <div className="action-row">
            <Link className="button button--primary" to="/incidents">
              Incydenty
            </Link>
            {hasPermission("incident.create") && (
              <Link className="button button--secondary" to="/incidents/new">
                Zgłoś incydent
              </Link>
            )}
          </div>
        ) : (
          <p className="muted">Nie masz dostępu do incydentów.</p>
        )}
      </div>
      <div className="panel">
        <h2>Administracja</h2>
        <div className="action-row">
          {links
            .filter(([, , permission]) => hasPermission(permission))
            .map(([to, label]) => (
              <Link key={to} className="button button--secondary" to={to}>
                {label}
              </Link>
            ))}
        </div>
        {!links.some(([, , permission]) => hasPermission(permission)) ? (
          <p className="muted">
            Nie masz dostępu do funkcji administracyjnych.
          </p>
        ) : null}
      </div>
    </section>
  );
}
