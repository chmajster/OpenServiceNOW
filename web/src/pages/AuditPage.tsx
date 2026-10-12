import { useEffect, useState, type FormEvent } from "react";
import { useSearchParams } from "react-router-dom";
import type { AuditRecord } from "../api/types";
import { useAuth } from "../auth/AuthContext";
import { RemoteCombobox, type Choice } from "../components/RemoteCombobox";
import { useRemotePage } from "../components/useRemotePage";
import {
  auditChanges,
  actionLabel,
  fieldLabel,
  displayValue,
} from "../components/auditPresentation";

const fields = [
  "from",
  "to",
  "actor_user_id",
  "action",
  "object_type",
  "object_id",
  "request_id",
] as const;
function localTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.valueOf())) return "";
  return new Date(date.valueOf() - date.getTimezoneOffset() * 60_000)
    .toISOString()
    .slice(0, 19);
}
export function AuditPage() {
  const { hasPermission } = useAuth();
  const [params, setParams] = useSearchParams();
  const key = params.toString();
  const [filters, setFilters] = useState<Record<string, string>>({});
  const [actor, setActor] = useState<Choice | null>(null);
  const [validation, setValidation] = useState<string | null>(null);
  useEffect(() => {
    const values = Object.fromEntries(
      fields.map((field) => [field, params.get(field) ?? ""]),
    );
    setFilters({
      ...values,
      from: localTime(values.from ?? ""),
      to: localTime(values.to ?? ""),
    });
    setActor(
      values.actor_user_id
        ? {
            id: values.actor_user_id,
            label:
              params.get("actor_label") ??
              `Użytkownik #${values.actor_user_id}`,
          }
        : null,
    );
  }, [key]);
  const query = new URLSearchParams({ limit: "50" });
  fields.forEach((field) => {
    const value = params.get(field);
    if (value) query.set(field, value);
  });
  const page = useRemotePage<AuditRecord>(
    `/api/v1/admin/audit/?${query}`,
    "events",
    "before_id",
  );
  function input(field: string, value: string) {
    setFilters((previous) => ({ ...previous, [field]: value }));
  }
  function submit(event: FormEvent) {
    event.preventDefault();
    setValidation(null);
    if (
      filters.from &&
      filters.to &&
      new Date(filters.from) > new Date(filters.to)
    ) {
      setValidation("Data początkowa nie może być późniejsza od końcowej.");
      return;
    }
    const result = new URLSearchParams();
    fields.forEach((field) => {
      const value = filters[field]?.trim();
      if (value)
        result.set(
          field,
          field === "from" || field === "to"
            ? new Date(value).toISOString()
            : value,
        );
    });
    if (actor && result.get("actor_user_id") === actor.id)
      result.set("actor_label", actor.label);
    setParams(result);
  }
  return (
    <section className="page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">Administracja</p>
          <h1>Audyt</h1>
          <p className="muted">
            Zdarzenia bieżącej organizacji, od najnowszych. Filtry są wykonywane
            przez serwer.
          </p>
        </div>
        <button
          className="button button--secondary"
          disabled={page.loading}
          onClick={page.reload}
        >
          Odśwież
        </button>
      </div>
      <div className="panel">
        <h2>Filtry audytu</h2>
        <form className="form-stack" onSubmit={submit}>
          <div className="filter-grid">
            <label>
              Data od
              <input
                type="datetime-local"
                step="1"
                value={filters.from ?? ""}
                onChange={(event) => input("from", event.target.value)}
              />
            </label>
            <label>
              Data do
              <input
                type="datetime-local"
                step="1"
                value={filters.to ?? ""}
                onChange={(event) => input("to", event.target.value)}
              />
            </label>
            {hasPermission("admin.users.manage") ? (
              <RemoteCombobox
                kind="users"
                label="Wyszukaj aktora"
                value={actor}
                allowInactive
                onChange={(value) => {
                  setActor(value);
                  input("actor_user_id", value?.id ?? "");
                }}
              />
            ) : null}
            <label>
              Identyfikator aktora
              <input
                value={filters.actor_user_id ?? ""}
                onChange={(event) => {
                  input("actor_user_id", event.target.value);
                  setActor(null);
                }}
                pattern="[0-9]+"
                maxLength={20}
                inputMode="numeric"
              />
            </label>
            <label>
              Akcja
              <input
                value={filters.action ?? ""}
                onChange={(event) => input("action", event.target.value)}
                maxLength={160}
                placeholder="np. tenant.name.update"
              />
            </label>
            <label>
              Typ obiektu
              <input
                value={filters.object_type ?? ""}
                onChange={(event) => input("object_type", event.target.value)}
                maxLength={100}
                placeholder="np. role"
              />
            </label>
            <label>
              Identyfikator obiektu
              <input
                value={filters.object_id ?? ""}
                onChange={(event) => input("object_id", event.target.value)}
                maxLength={128}
              />
            </label>
            <label>
              Identyfikator żądania (request_id)
              <input
                value={filters.request_id ?? ""}
                onChange={(event) => input("request_id", event.target.value)}
                maxLength={128}
              />
            </label>
          </div>
          <p className="muted">
            Daty w Twojej strefie czasowej; pozostałe filtry wymagają dokładnej
            wartości.
          </p>
          {validation ? (
            <p className="inline-error" role="alert">
              {validation}
            </p>
          ) : null}
          <div className="action-row">
            <button className="button button--primary">Zastosuj filtry</button>
            <button
              type="button"
              className="button button--secondary"
              onClick={() => {
                setParams({});
                setValidation(null);
              }}
            >
              Wyczyść filtry
            </button>
          </div>
        </form>
      </div>
      {page.error ? (
        <div className="inline-error" role="alert">
          {page.error}{" "}
          <button
            className="button button--secondary"
            onClick={() => void page.retry()}
          >
            Spróbuj ponownie
          </button>
        </div>
      ) : null}
      <div className="panel">
        {page.loading ? <p role="status">Ładowanie audytu…</p> : null}
        <div className="audit-list">
          {page.items.map((event) => (
            <AuditEntry key={event.id} event={event} />
          ))}
        </div>
        {!page.loading && !page.error && !page.items.length ? (
          <div className="empty-cell">Brak zdarzeń dla podanych kryteriów.</div>
        ) : null}
        {page.next ? (
          <div className="panel-footer">
            <button
              className="button button--secondary"
              disabled={page.loading}
              onClick={() => void page.more()}
            >
              Załaduj starsze
            </button>
          </div>
        ) : null}
      </div>
    </section>
  );
}
function AuditEntry({ event }: { event: AuditRecord }) {
  const changes = auditChanges(event.old_values, event.new_values);
  return (
    <article className="audit-entry">
      <div className="audit-meta">
        <strong>{actionLabel(event.action)}</strong>
        <span>{new Date(event.time).toLocaleString("pl-PL")}</span>
      </div>
      <div className="audit-object">
        <span>{fieldLabel(event.object_type)}</span>
        {event.object_id ? <code>#{event.object_id}</code> : null}
        <span>
          {event.actor_user_id
            ? `Użytkownik #${event.actor_user_id}`
            : "System"}
        </span>
      </div>
      {changes.length ? (
        <div className="table-wrap" tabIndex={0} aria-label="Różnice pól">
          <table>
            <thead>
              <tr>
                <th>Pole</th>
                <th>Przed</th>
                <th>Po</th>
              </tr>
            </thead>
            <tbody>
              {changes.map((change) => (
                <tr key={change.field}>
                  <td>{fieldLabel(change.field)}</td>
                  <td className="wrap-cell">{displayValue(change.before)}</td>
                  <td className="wrap-cell">{displayValue(change.after)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
      <details>
        <summary>Szczegóły techniczne</summary>
        <dl className="detail-grid">
          <div>
            <dt>Akcja</dt>
            <dd>{event.action}</dd>
          </div>
          <div>
            <dt>Identyfikator żądania</dt>
            <dd>{event.request_id || "—"}</dd>
          </div>
          <div>
            <dt>Źródło</dt>
            <dd>{event.source}</dd>
          </div>
          <div>
            <dt>Adres IP</dt>
            <dd>{event.ip_address || "—"}</dd>
          </div>
        </dl>
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
    </article>
  );
}
