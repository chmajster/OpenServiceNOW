import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { HealthResponse } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import { PageState } from "../components/PageState";

export function OverviewPage() {
  const { session } = useAuth();
  const [health, setHealth] = useState<HealthResponse | null>(null);
  const [healthError, setHealthError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    void api<HealthResponse>("/health/ready")
      .then((result) => {
        if (!cancelled) {
          setHealth(result);
          setHealthError(null);
        }
      })
      .catch((cause: unknown) => {
        if (!cancelled) {
          setHealthError(cause instanceof Error ? cause.message : "Healthcheck nie powiódł się");
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <section className="page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">Przegląd</p>
          <h1>Fundament platformy</h1>
          <p className="muted">Bieżący tenant: {session?.tenant.name}</p>
        </div>
      </div>

      <div className="metric-grid">
        <article className="metric-card">
          <span>API</span>
          <strong>{health?.status === "ok" ? "Gotowe" : healthError ? "Błąd" : "Sprawdzanie"}</strong>
          <small>/health/ready</small>
        </article>
        <article className="metric-card">
          <span>MySQL</span>
          <strong>{health?.checks?.mysql ?? "—"}</strong>
          <small>System of record</small>
        </article>
        <article className="metric-card">
          <span>Redis</span>
          <strong>{health?.checks?.redis ?? "—"}</strong>
          <small>Rate limiting i koordynacja</small>
        </article>
        <article className="metric-card">
          <span>Uprawnienia</span>
          <strong>{session?.permissions.length ?? 0}</strong>
          <small>Efektywne permission keys</small>
        </article>
      </div>

      {healthError ? (
        <PageState title="Backend nie jest gotowy" detail={healthError} tone="error" />
      ) : null}

      <div className="panel">
        <div className="panel-heading">
          <div>
            <h2>Aktywna sesja</h2>
            <p className="muted">Tożsamość i tenant są ustalane po stronie backendu.</p>
          </div>
        </div>
        <dl className="detail-grid">
          <div><dt>Użytkownik</dt><dd>{session?.user.display_name}</dd></div>
          <div><dt>E-mail</dt><dd>{session?.user.email}</dd></div>
          <div><dt>Tenant</dt><dd>{session?.tenant.slug}</dd></div>
          <div><dt>Wygasa</dt><dd>{session ? new Date(session.expires_at).toLocaleString("pl-PL") : "—"}</dd></div>
        </dl>
      </div>
    </section>
  );
}
