import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { AuditPage as AuditPageResponse, AuditRecord } from "../api/types";
import { PageState } from "../components/PageState";

export function AuditPage() {
  const [events, setEvents] = useState<AuditRecord[]>([]);
  const [nextBeforeID, setNextBeforeID] = useState<number | undefined>();
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function load(beforeID?: number, append = false) {
    append ? setLoadingMore(true) : setLoading(true);
    try {
      const query = new URLSearchParams({ limit: "50" });
      if (beforeID) {
        query.set("before_id", String(beforeID));
      }
      const response = await api<AuditPageResponse>(`/api/v1/admin/audit/?${query.toString()}`);
      setEvents((current) => (append ? [...current, ...response.events] : response.events));
      setNextBeforeID(response.next_before_id);
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Nie udało się pobrać audytu.");
    } finally {
      setLoading(false);
      setLoadingMore(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  if (loading && events.length === 0) {
    return <PageState title="Ładowanie audytu" />;
  }

  return (
    <section className="page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">Administracja</p>
          <h1>Audyt</h1>
          <p className="muted">Zdarzenia bieżącego tenanta, od najnowszych.</p>
        </div>
        <button className="button button--secondary" onClick={() => void load()} disabled={loading}>
          Odśwież
        </button>
      </div>

      {error ? <div className="inline-error" role="alert">{error}</div> : null}

      <div className="panel">
        <div className="audit-list">
          {events.map((event) => (
            <article className="audit-entry" key={event.id}>
              <div className="audit-meta">
                <strong>{event.action}</strong>
                <span>{new Date(event.time).toLocaleString("pl-PL")}</span>
              </div>
              <div className="audit-object">
                <span>{event.object_type}</span>
                {event.object_id ? <code>#{event.object_id}</code> : null}
                {event.actor_user_id ? <span>użytkownik {event.actor_user_id}</span> : null}
                <span>{event.source}</span>
              </div>
              {event.new_values !== undefined || event.old_values !== undefined ? (
                <details>
                  <summary>Szczegóły zmian</summary>
                  <div className="audit-values">
                    <div>
                      <strong>Przed</strong>
                      <pre>{JSON.stringify(event.old_values ?? null, null, 2)}</pre>
                    </div>
                    <div>
                      <strong>Po</strong>
                      <pre>{JSON.stringify(event.new_values ?? null, null, 2)}</pre>
                    </div>
                  </div>
                </details>
              ) : null}
              {event.request_id ? <small className="muted">request_id: {event.request_id}</small> : null}
            </article>
          ))}
          {events.length === 0 ? <div className="empty-cell">Brak zdarzeń audytowych.</div> : null}
        </div>

        {nextBeforeID ? (
          <div className="panel-footer">
            <button
              className="button button--secondary"
              disabled={loadingMore}
              onClick={() => void load(nextBeforeID, true)}
            >
              {loadingMore ? "Ładowanie…" : "Załaduj starsze"}
            </button>
          </div>
        ) : null}
      </div>
    </section>
  );
}
