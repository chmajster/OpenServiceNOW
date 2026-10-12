import { useState, type ReactNode } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { api, jsonBody, ApiError } from "../api/client";
import { PageState } from "../components/PageState";
import { ConflictNotice } from "../components/ConflictNotice";
import { useIncidentQuery } from "../incidents/hooks";
import { useAuth } from "../auth/AuthContext";
import {
  deliveryLabels,
  errorLabels,
  type Preference,
  type NotificationPage,
  type MailDelivery,
} from "../notifications/contracts";

export function NotificationsPage() {
  const { session } = useAuth();
  return (
    <NotificationWorkspace
      key={`${session?.tenant.id}:${session?.user.id}:${session?.expires_at}`}
    />
  );
}
function NotificationWorkspace() {
  const [params, setParams] = useSearchParams();
  const tab = params.get("tab") ?? "list";
  const before = params.get("before_id") ?? "";
  const query = useIncidentQuery<NotificationPage>(
    `/api/v1/notifications/?limit=25${before ? `&before_id=${encodeURIComponent(before)}` : ""}`,
    tab,
  );
  const [busy, setBusy] = useState(false),
    [error, setError] = useState<string>();
  async function read(id?: string) {
    setBusy(true);
    setError(undefined);
    try {
      await api(`/api/v1/notifications/${id ? `${id}/read` : "read-all"}`, {
        method: "POST",
      });
      query.reload();
      window.dispatchEvent(new Event("notifications-changed"));
    } catch (cause) {
      setError(
        cause instanceof Error
          ? cause.message
          : "Nie udało się oznaczyć odczytu",
      );
    } finally {
      setBusy(false);
    }
  }
  function select(next: string) {
    setParams({ tab: next });
  }
  return (
    <section className="page notification-page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">Moje konto</p>
          <h1>Powiadomienia</h1>
          <p className="muted">
            Widoczne są wyłącznie zdarzenia rekordów, do których nadal masz
            dostęp. Projekcja może pojawić się z opóźnieniem.
          </p>
        </div>
      </div>
      <nav className="action-row tabs" aria-label="Widok powiadomień">
        {(
          [
            ["list", "Lista"],
            ["preferences", "Preferencje"],
            ["mail", "Moje dostawy SMTP"],
          ] as const
        ).map(([id, label]) => (
          <button
            key={id}
            className="button button--secondary"
            aria-current={tab === id ? "page" : undefined}
            onClick={() => select(id)}
          >
            {label}
          </button>
        ))}
      </nav>
      {tab === "preferences" ? (
        <Preferences />
      ) : tab === "mail" ? (
        <OwnMail
          before={before}
          next={(cursor) =>
            setParams({ tab: "mail", ...(cursor ? { before_id: cursor } : {}) })
          }
        />
      ) : (
        <div className="panel">
          <div className="action-row">
            <strong>Nieprzeczytane: {query.data?.unread ?? "—"}</strong>
            <button
              className="button button--secondary"
              disabled={busy || query.loading}
              onClick={() => {
                query.reload();
                window.dispatchEvent(new Event("notifications-changed"));
              }}
            >
              Odśwież powiadomienia
            </button>
            <button
              className="button button--secondary"
              disabled={busy || !query.data || query.data.unread === "0"}
              onClick={() => void read()}
            >
              Oznacz wszystkie jako przeczytane
            </button>
          </div>
          {error && (
            <PageState tone="error" title="Błąd odczytu" detail={error} />
          )}
          {query.error && (
            <PageState
              tone="error"
              title="Nie pobrano powiadomień"
              detail={query.error}
            />
          )}{" "}
          {query.loading ? (
            <PageState title="Ładowanie powiadomień…" />
          ) : (
            query.data && (
              <>
                {!query.data.notifications.length ? (
                  <PageState title="Brak powiadomień" />
                ) : (
                  <ul className="notification-list">
                    {query.data.notifications.map((n) => (
                      <li
                        key={n.id}
                        className={n.read_at ? "" : "notification-unread"}
                      >
                        <div>
                          <strong>{n.summary}</strong>
                          <p>
                            <time dateTime={n.occurred_at}>
                              {new Date(n.occurred_at).toLocaleString("pl-PL")}
                            </time>
                          </p>
                          <Link
                            to={`/incidents/${n.incident_id}`}
                            onClick={() => {
                              if (!n.read_at) void read(n.id);
                            }}
                          >
                            Otwórz incydent #{n.incident_id}
                          </Link>
                        </div>
                        <button
                          className="button button--small button--secondary"
                          disabled={busy || !!n.read_at}
                          onClick={() => void read(n.id)}
                        >
                          {n.read_at
                            ? "Przeczytane"
                            : "Oznacz jako przeczytane"}
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
                <div className="action-row">
                  {before && (
                    <button
                      className="button button--secondary"
                      onClick={() => setParams({ tab: "list" })}
                    >
                      Najnowsze
                    </button>
                  )}
                  {query.data.next_before_id && (
                    <button
                      className="button button--secondary"
                      onClick={() =>
                        setParams({
                          tab: "list",
                          before_id: query.data!.next_before_id!,
                        })
                      }
                    >
                      Starsze powiadomienia
                    </button>
                  )}
                </div>
              </>
            )
          )}
        </div>
      )}
    </section>
  );
}
function Preferences() {
  const query = useIncidentQuery<Preference>(
    "/api/v1/notifications/preferences",
  );
  return query.data ? (
    <PreferenceForm initial={query.data} />
  ) : (
    <PageState
      title={query.error ?? "Ładowanie preferencji…"}
      tone={query.error ? "error" : "neutral"}
    />
  );
}
function PreferenceForm({ initial }: { initial: Preference }) {
  const [draft, setDraft] = useState(initial),
    [busy, setBusy] = useState(false),
    [error, setError] = useState<string>(),
    [success, setSuccess] = useState(false),
    [current, setCurrent] = useState<Preference>();
  async function save() {
    setBusy(true);
    setError(undefined);
    setSuccess(false);
    try {
      await api("/api/v1/notifications/preferences", {
        method: "PUT",
        body: jsonBody(draft),
      });
      const fresh = await api<Preference>("/api/v1/notifications/preferences");
      setDraft(fresh);
      setSuccess(true);
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : "Nie potwierdzono zapisu",
      );
      if (cause instanceof ApiError && cause.status === 409) {
        try {
          setCurrent(
            await api<Preference>("/api/v1/notifications/preferences"),
          );
        } catch {
          setError(
            "Propozycja zachowana. Odśwież aktualny stan przed kolejnym zapisem.",
          );
        }
      }
    } finally {
      setBusy(false);
    }
  }
  const labels: [keyof Omit<Preference, "version">, string][] = [
    ["in_app_enabled", "Powiadomienia w aplikacji"],
    [
      "smtp_enabled",
      "Powiadomienia e-mail (wymagają konfiguracji administratora)",
    ],
    ["assignments", "Przypisanie incydentu"],
    ["replies", "Odpowiedzi i dozwolone notatki"],
    ["state_changes", "Zmiany stanu"],
    ["resolutions", "Rozwiązanie incydentu"],
  ];
  return (
    <form
      className="panel form-stack notification-form"
      onSubmit={(e) => {
        e.preventDefault();
        void save();
      }}
    >
      <h2>Preferencje w tej organizacji</h2>
      {labels.map(([key, label]) => (
        <label className="checkbox-row" key={key}>
          <input
            type="checkbox"
            checked={draft[key]}
            disabled={busy}
            onChange={(e) => {
              setDraft({ ...draft, [key]: e.target.checked });
              setSuccess(false);
            }}
          />
          {label}
        </label>
      ))}
      {error && (
        <PageState
          tone="error"
          title="Nie zapisano preferencji"
          detail={error}
        />
      )}{" "}
      {success && <PageState tone="success" title="Zapisano preferencje" />}
      {current && (
        <ConflictNotice
          current={<pre>{JSON.stringify(current, null, 2)}</pre>}
          proposal={<pre>{JSON.stringify(draft, null, 2)}</pre>}
          acceptCurrent={() => {
            setDraft(current);
            setCurrent(undefined);
            setError(undefined);
          }}
          keepProposal={() => {
            setDraft({ ...draft, version: current.version });
            setCurrent(undefined);
            setError(undefined);
          }}
        />
      )}
      <button className="button button--primary" disabled={busy || !!current}>
        {busy ? "Zapisywanie…" : "Zapisz preferencje"}
      </button>
    </form>
  );
}
function OwnMail({
  before,
  next,
}: {
  before: string;
  next: (cursor?: string) => void;
}) {
  const query = useIncidentQuery<{
    deliveries: MailDelivery[];
    next_before_id?: string;
  }>(
    `/api/v1/notifications/mail-history?limit=25${before ? `&before_id=${encodeURIComponent(before)}` : ""}`,
  );
  return (
    <div className="panel">
      <h2>Moje dostawy SMTP</h2>
      <button className="button button--secondary" onClick={query.reload}>
        Odśwież dostawy
      </button>
      {query.loading ? (
        <PageState title="Ładowanie dostaw…" />
      ) : query.error ? (
        <PageState
          tone="error"
          title="Nie pobrano dostaw"
          detail={query.error}
        />
      ) : (
        <>
          <MailTable deliveries={query.data?.deliveries ?? []} />
          {before && (
            <button className="button button--secondary" onClick={() => next()}>
              Najnowsze
            </button>
          )}
          {query.data?.next_before_id && (
            <button
              className="button button--secondary"
              onClick={() => next(query.data!.next_before_id)}
            >
              Starsze dostawy
            </button>
          )}
        </>
      )}
    </div>
  );
}
export function MailTable({
  deliveries,
  actions,
}: {
  deliveries: MailDelivery[];
  actions?: (d: MailDelivery) => ReactNode;
}) {
  return !deliveries.length ? (
    <PageState title="Brak dostaw SMTP" />
  ) : (
    <div className="table-scroll" tabIndex={0} aria-label="Tabela dostaw">
      <table>
        <thead>
          <tr>
            <th>Dostawa</th>
            <th>Stan</th>
            <th>Próby</th>
            <th>Dostawca</th>
            <th>Błąd</th>
            {actions && <th>Akcje</th>}
          </tr>
        </thead>
        <tbody>
          {deliveries.map((d) => (
            <tr key={d.id}>
              <td>
                #{d.id}
                <small className="muted">
                  {" "}
                  {new Date(d.created_at).toLocaleString("pl-PL")}
                </small>
              </td>
              <td>{deliveryLabels[d.status] ?? d.status}</td>
              <td>{d.attempt_count}</td>
              <td>{d.provider_id || "—"}</td>
              <td>{(errorLabels[d.error_code] ?? d.error_code) || "—"}</td>
              {actions && <td>{actions(d)}</td>}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
