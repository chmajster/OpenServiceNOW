import { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { api, ApiError, jsonBody } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { PageState } from "../components/PageState";
import { ConflictNotice } from "../components/ConflictNotice";
import { useIncidentQuery } from "../incidents/hooks";
import { UnsavedGuard } from "../incidents/UnsavedGuard";
import { MailTable } from "./NotificationsPage";
import {
  deliveryLabels,
  errorLabels,
  type SMTPConfig,
  type MailDelivery,
  type EventDelivery,
  type Attempt,
} from "../notifications/contracts";

export function NotificationAdminPage() {
  const { session } = useAuth();
  return (
    <AdminWorkspace
      key={`${session?.tenant.id}:${session?.user.id}:${session?.expires_at}`}
    />
  );
}
function AdminWorkspace() {
  const [params, setParams] = useSearchParams();
  const tab = params.get("tab") ?? "smtp",
    before = params.get("before_id") ?? "";
  return (
    <section className="page notification-page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">Administracja</p>
          <h1>Dostarczanie powiadomień</h1>
          <p className="muted">
            Każdy odbiorca ma własny wynik. Zakolejkowanie zadania nie oznacza
            dostarczenia.
          </p>
        </div>
      </div>
      <nav className="action-row tabs" aria-label="Administracja powiadomień">
        {(
          [
            ["smtp", "Konfiguracja SMTP"],
            ["events", "Odbiorcy zdarzeń"],
            ["mail", "Dostawy SMTP"],
          ] as const
        ).map(([id, label]) => (
          <button
            key={id}
            className="button button--secondary"
            aria-current={tab === id ? "page" : undefined}
            onClick={() => setParams({ tab: id })}
          >
            {label}
          </button>
        ))}
      </nav>
      <div hidden={tab !== "smtp"}>
        <Configuration />
      </div>
      {tab !== "smtp" ? (
        <DeliveryQueue
          key={tab}
          kind={tab === "events" ? "events" : "mail"}
          before={before}
          next={(cursor) =>
            setParams({ tab, ...(cursor ? { before_id: cursor } : {}) })
          }
        />
      ) : null}
    </section>
  );
}
function Configuration() {
  const query = useIncidentQuery<SMTPConfig>(
    "/api/v1/admin/notifications/smtp",
  );
  return query.data ? (
    <ConfigurationForm initial={query.data} />
  ) : (
    <PageState
      title={query.error ?? "Ładowanie konfiguracji…"}
      tone={query.error ? "error" : "neutral"}
    />
  );
}
function safeConfig(c: SMTPConfig) {
  return `${c.provider_id} · ${c.host}:${c.port} · ${c.tls_mode} · ${c.from_address} · ${c.enabled ? "włączony" : "wyłączony"} · hasło ${c.has_password ? "ustawione" : "brak"}`;
}
function ConfigurationForm({ initial }: { initial: SMTPConfig }) {
  const [draft, setDraft] = useState(initial),
    [password, setPassword] = useState(""),
    [replacePassword, setReplacePassword] = useState(false),
    [dirty, setDirty] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState<string>(),
    [feedback, setFeedback] = useState<string>(),
    [current, setCurrent] = useState<SMTPConfig>(),
    [testKey, setTestKey] = useState<string>();
  function field<K extends keyof SMTPConfig>(key: K, value: SMTPConfig[K]) {
    setDraft({ ...draft, [key]: value });
    setDirty(true);
    setFeedback(undefined);
  }
  async function save() {
    setBusy(true);
    setError(undefined);
    setFeedback(undefined);
    try {
      await api("/api/v1/admin/notifications/smtp", {
        method: "PUT",
        body: jsonBody({
          provider_id: draft.provider_id,
          enabled: draft.enabled,
          host: draft.host,
          port: draft.port,
          tls_mode: draft.tls_mode,
          username: draft.username,
          from_address: draft.from_address,
          version: draft.version,
          ...(replacePassword ? { password } : {}),
        }),
      });
      const fresh = await api<SMTPConfig>("/api/v1/admin/notifications/smtp");
      setDraft(fresh);
      setPassword("");
      setReplacePassword(false);
      setDirty(false);
      setFeedback("Zapisano konfigurację SMTP");
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : "Nie potwierdzono zapisu",
      );
      if (cause instanceof ApiError && cause.status === 409) {
        try {
          setCurrent(await api<SMTPConfig>("/api/v1/admin/notifications/smtp"));
        } catch {
          setError(
            "Nie odczytano aktualnej konfiguracji. Propozycja pozostaje w formularzu.",
          );
        }
      }
    } finally {
      setBusy(false);
    }
  }
  async function test() {
    const key = testKey ?? crypto.randomUUID();
    setTestKey(key);
    setBusy(true);
    setError(undefined);
    try {
      const result = await api<{ delivery_id: string }>(
        "/api/v1/admin/notifications/smtp/test",
        { method: "POST", headers: { "Idempotency-Key": key } },
      );
      setFeedback(
        `Próba #${result.delivery_id} została zakolejkowana na adres Twojego konta. Sprawdź rzeczywisty wynik w Dostawach SMTP.`,
      );
      setTestKey(undefined);
    } catch (cause) {
      setError(
        cause instanceof Error
          ? cause.message
          : "Nie potwierdzono próby; ponowienie użyje tego samego klucza",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <UnsavedGuard dirty={dirty} />
      <form
        className="panel form-stack notification-form"
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <h2>Opcjonalny kanał SMTP</h2>
        <p className="muted">
          Kanał w aplikacji działa niezależnie. SMTP może wysłać duplikat po
          przyjęciu wiadomości przez serwer i utracie potwierdzenia.
        </p>
        <fieldset disabled={busy} className="form-stack">
          <label className="checkbox-row">
            <input
              type="checkbox"
              checked={draft.enabled}
              onChange={(e) => field("enabled", e.target.checked)}
            />
            Włącz kanał SMTP
          </label>
          <label>
            Identyfikator dostawcy
            <input
              required
              maxLength={100}
              pattern="[A-Za-z0-9][A-Za-z0-9._-]*"
              value={draft.provider_id}
              onChange={(e) => field("provider_id", e.target.value)}
            />
          </label>
          <label>
            Serwer SMTP
            <input
              required
              maxLength={253}
              value={draft.host}
              onChange={(e) => field("host", e.target.value)}
            />
          </label>
          <label>
            Port
            <input
              required
              type="number"
              min={1}
              max={65535}
              value={draft.port}
              onChange={(e) => field("port", Number(e.target.value))}
            />
          </label>
          <label>
            Szyfrowanie połączenia
            <select
              value={draft.tls_mode}
              onChange={(e) => field("tls_mode", e.target.value)}
            >
              <option value="STARTTLS">STARTTLS</option>
              <option value="TLS">TLS</option>
              {draft.insecure_allowed && (
                <option value="NONE">Bez TLS — lokalny serwer testowy</option>
              )}
            </select>
          </label>
          <label>
            Adres nadawcy
            <input
              required
              type="email"
              maxLength={254}
              value={draft.from_address}
              onChange={(e) => field("from_address", e.target.value)}
            />
          </label>
          <label>
            Login SMTP (opcjonalny)
            <input
              maxLength={254}
              autoComplete="off"
              value={draft.username}
              onChange={(e) => field("username", e.target.value)}
            />
          </label>
          <p>
            Hasło:{" "}
            {draft.has_password ? "zapisane i zaszyfrowane" : "nieustawione"}.
            Klucz poza bazą:{" "}
            {draft.encryption_ready ? "dostępny" : "nieskonfigurowany"}.
          </p>
          <label className="checkbox-row">
            <input
              type="checkbox"
              checked={replacePassword}
              onChange={(e) => {
                setReplacePassword(e.target.checked);
                setDirty(true);
              }}
            />
            Zmień lub usuń zapisane hasło
          </label>
          {replacePassword && (
            <label>
              Nowe hasło SMTP (puste usuwa hasło)
              <input
                type="password"
                maxLength={1024}
                autoComplete="new-password"
                value={password}
                onChange={(e) => {
                  setPassword(e.target.value);
                  setDirty(true);
                }}
              />
            </label>
          )}
        </fieldset>
        {error && (
          <PageState
            tone="error"
            title="Nie wykonano operacji"
            detail={error}
          />
        )}{" "}
        {feedback && <PageState tone="success" title={feedback} />}{" "}
        {current && (
          <ConflictNotice
            current={<p>{safeConfig(current)}</p>}
            proposal={
              <p>
                {safeConfig(draft)}
                {replacePassword
                  ? " · lokalna zmiana hasła zachowana (treść ukryta)"
                  : ""}
              </p>
            }
            acceptCurrent={() => {
              setDraft(current);
              setPassword("");
              setReplacePassword(false);
              setDirty(false);
              setCurrent(undefined);
              setError(undefined);
            }}
            keepProposal={() => {
              setDraft({
                ...draft,
                version: current.version,
                has_password: current.has_password,
              });
              setCurrent(undefined);
              setError(undefined);
            }}
          />
        )}
        <div className="action-row">
          <button
            className="button button--primary"
            disabled={busy || !!current}
          >
            {busy ? "Przetwarzanie…" : "Zapisz konfigurację"}
          </button>
          <button
            type="button"
            className="button button--secondary"
            disabled={busy || dirty || !draft.enabled || draft.version === "0"}
            onClick={() => void test()}
          >
            {testKey ? "Sprawdź lub ponów próbę" : "Wyślij próbę na mój adres"}
          </button>
        </div>
      </form>
    </>
  );
}
function DeliveryQueue({
  kind,
  before,
  next,
}: {
  kind: "events" | "mail";
  before: string;
  next: (cursor?: string) => void;
}) {
  const path = `/api/v1/admin/notifications/${kind === "events" ? "deliveries" : "mail"}`;
  const query = useIncidentQuery<{
    deliveries: (MailDelivery | EventDelivery)[];
    next_before_id?: string;
  }>(
    `${path}?limit=25${before ? `&before_id=${encodeURIComponent(before)}` : ""}`,
  );
  const [busy, setBusy] = useState(false),
    [error, setError] = useState<string>(),
    [confirm, setConfirm] = useState<MailDelivery | EventDelivery>(),
    [attemptID, setAttemptID] = useState<string>();
  async function retry(d: MailDelivery | EventDelivery) {
    setBusy(true);
    setError(undefined);
    try {
      await api(`${path}/${d.id}/retry`, {
        method: "POST",
        body: jsonBody(
          kind === "events"
            ? { attempt_count: d.attempt_count }
            : { version: (d as MailDelivery).version },
        ),
      });
      setConfirm(undefined);
      query.reload();
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : "Nie potwierdzono ponowienia",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="panel">
      <h2>
        {kind === "events" ? "Niezależni odbiorcy zdarzeń" : "Dostawy SMTP"}
      </h2>
      <button
        className="button button--secondary"
        disabled={busy}
        onClick={query.reload}
      >
        Odśwież kolejkę
      </button>
      {error && <PageState tone="error" title="Błąd dostawy" detail={error} />}{" "}
      {confirm && (
        <section
          className="conflict-notice"
          aria-label="Potwierdzenie ponowienia"
        >
          <p>
            Ponowić dostawę #{confirm.id}? Ponowienie SMTP może wysłać duplikat.
            Dostawy zakończone sukcesem pozostają bez zmian.
          </p>
          <div className="action-row">
            <button
              className="button button--primary"
              disabled={busy}
              onClick={() => void retry(confirm)}
            >
              Potwierdź ponowienie
            </button>
            <button
              className="button button--secondary"
              disabled={busy}
              onClick={() => setConfirm(undefined)}
            >
              Anuluj
            </button>
          </div>
        </section>
      )}
      {query.loading ? (
        <PageState title="Ładowanie kolejki…" />
      ) : query.error ? (
        <PageState
          tone="error"
          title="Nie pobrano kolejki"
          detail={query.error}
        />
      ) : kind === "mail" ? (
        <MailTable
          deliveries={(query.data?.deliveries ?? []) as MailDelivery[]}
          actions={(d) => (
            <div className="action-row">
              <button
                className="button button--small button--secondary"
                disabled={busy}
                onClick={() => setAttemptID(d.id)}
              >
                Historia prób
              </button>
              {["FAILED", "NOT_CONFIGURED"].includes(d.status) &&
                d.attempt_count < 100 && (
                  <button
                    className="button button--small button--secondary"
                    disabled={busy || !!confirm}
                    onClick={() => setConfirm(d)}
                  >
                    Ponów
                  </button>
                )}
            </div>
          )}
        />
      ) : !query.data?.deliveries.length ? (
        <PageState title="Brak dostaw zdarzeń" />
      ) : (
        <div className="table-scroll" tabIndex={0} aria-label="Tabela dostaw">
          <table>
            <thead>
              <tr>
                <th>Zdarzenie</th>
                <th>Odbiorca</th>
                <th>Stan</th>
                <th>Próby</th>
                <th>Błąd</th>
                <th>Akcje</th>
              </tr>
            </thead>
            <tbody>
              {(query.data.deliveries as EventDelivery[]).map((d) => (
                <tr key={d.id}>
                  <td>#{d.event_id}</td>
                  <td>{d.consumer_id}</td>
                  <td>{deliveryLabels[d.status] ?? d.status}</td>
                  <td>
                    {d.attempt_count} / {d.max_attempts}
                  </td>
                  <td>{errorLabels[d.last_error] ?? d.last_error}</td>
                  <td>
                    {d.status === "FAILED" && d.attempt_count < 100 && (
                      <button
                        className="button button--small button--secondary"
                        disabled={busy || !!confirm}
                        onClick={() => setConfirm(d)}
                      >
                        Ponów
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {attemptID && (
        <Attempts id={attemptID} close={() => setAttemptID(undefined)} />
      )}
      <div className="action-row">
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
      </div>
    </div>
  );
}
function Attempts({ id, close }: { id: string; close: () => void }) {
  const query = useIncidentQuery<{ attempts: Attempt[] }>(
    `/api/v1/admin/notifications/mail/${id}/attempts`,
  );
  return (
    <section className="panel">
      <h3>Historia prób dostawy #{id}</h3>
      <button className="button button--secondary" onClick={close}>
        Zamknij historię
      </button>
      {query.loading ? (
        <PageState title="Ładowanie prób…" />
      ) : query.error ? (
        <PageState tone="error" title="Nie pobrano prób" detail={query.error} />
      ) : !query.data?.attempts.length ? (
        <PageState title="Nie wykonano próby wysyłki" />
      ) : (
        <ul>
          {query.data.attempts.map((a) => (
            <li key={a.attempt_number}>
              Próba {a.attempt_number}: {deliveryLabels[a.status] ?? a.status} ·{" "}
              {new Date(a.started_at).toLocaleString("pl-PL")} ·{" "}
              {errorLabels[a.error_code] ?? a.error_code}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
