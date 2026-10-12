import { useEffect, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { api, jsonBody } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { PageState } from "../components/PageState";
import { RemoteCombobox, type Choice } from "../components/RemoteCombobox";
import { useIncidentQuery } from "./hooks";
import { dateLabel, type Incident } from "./contracts";
import type { Access } from "./CommunicationPanel";

export function ParticipantsPanel({ record }: { record: Incident }) {
  const { session } = useAuth();
  const base = `/api/v1/incidents/${record.id}`;
  const [params, setParams] = useSearchParams();
  const access = useIncidentQuery<Access>(`${base}/communication-access`);
  const watchers = useIncidentQuery<{
    watchers: { user_id: string; display_name: string; created_at: string }[];
    next_after_id?: string;
    watching: boolean;
  }>(
    `${base}/watchers?limit=20&after_id=${params.get("watchers_after_id") ?? ""}`,
  );
  const relations = useIncidentQuery<{
    relations: {
      id: string;
      type: string;
      source_id: string;
      target_id: string;
      other_id: string;
      number: string;
      title: string;
    }[];
    next_before_id?: string;
  }>(
    `${base}/relations?limit=20&before_id=${params.get("relations_before_id") ?? ""}`,
  );
  const [user, setUser] = useState<Choice | null>(null),
    [target, setTarget] = useState<Choice | null>(null),
    [kind, setKind] = useState("RELATED");
  const [busy, setBusy] = useState(false),
    [error, setError] = useState<string>(),
    [notice, setNotice] = useState<string>();
  const mounted = useRef(true);
  const request = useRef<AbortController | null>(null);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      request.current?.abort();
    };
  }, []);
  async function change(path: string, method: string, body?: object) {
    setBusy(true);
    setError(undefined);
    request.current = new AbortController();
    try {
      await api(`${base}/${path}`, {
        method,
        body: body ? jsonBody(body) : undefined,
        signal: request.current.signal,
      });
      if (mounted.current) {
        watchers.reload();
        relations.reload();
        setUser(null);
        setTarget(null);
        setNotice(
          "Zmiana zapisana. Aktywność może jeszcze oczekiwać na worker.",
        );
      }
    } catch (cause) {
      if (mounted.current)
        setError(
          cause instanceof Error
            ? cause.message
            : "Nie udało się zapisać zmiany.",
        );
    } finally {
      if (mounted.current) setBusy(false);
    }
  }
  const page = (key: string, value?: string) => {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    setParams(next);
  };
  return (
    <>
      <h2>Obserwatorzy i powiązania</h2>
      {error && (
        <p className="notice notice--error" role="alert">
          {error}
        </p>
      )}
      {notice && (
        <p className="notice" role="status">
          {notice}
        </p>
      )}
      <h3>Obserwatorzy</h3>
      <p className="muted">
        Obserwowanie nie nadaje dostępu do incydentu. Dostarczanie powiadomień
        będzie osobnym etapem.
      </p>
      <button
        className="button button--secondary"
        disabled={busy || watchers.loading || !watchers.data}
        onClick={() =>
          void change(
            `watchers/${session!.user.id}`,
            watchers.data?.watching ? "DELETE" : "PUT",
          )
        }
      >
        {watchers.data?.watching ? "Przestań obserwować" : "Obserwuj incydent"}
      </button>
      {access.data?.manage && (
        <form
          onSubmit={(event) => {
            event.preventDefault();
            if (user) void change(`watchers/${user.id}`, "PUT");
          }}
        >
          <RemoteCombobox
            kind="users"
            label="Nowy obserwator"
            endpoint="/api/v1/incidents/lookups/users?purpose=affected"
            value={user}
            onChange={setUser}
            disabled={busy}
          />
          <button className="button button--primary" disabled={busy || !user}>
            Dodaj obserwatora
          </button>
        </form>
      )}
      {watchers.loading ? (
        <PageState title="Ładowanie obserwatorów…" />
      ) : watchers.error ? (
        <PageState
          title="Błąd odczytu obserwatorów"
          detail={watchers.error}
          tone="error"
        />
      ) : !watchers.data?.watchers.length ? (
        <PageState title="Brak dostępnych obserwatorów" />
      ) : (
        <ul>
          {watchers.data.watchers.map((w) => (
            <li key={w.user_id}>
              {w.display_name} · {dateLabel(w.created_at)}{" "}
              {(access.data?.manage || w.user_id === session?.user.id) && (
                <button
                  className="button button--secondary"
                  disabled={busy}
                  onClick={() => void change(`watchers/${w.user_id}`, "DELETE")}
                >
                  Usuń obserwatora {w.display_name}
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      <div className="action-row">
        <button
          className="button button--secondary"
          disabled={busy}
          onClick={watchers.reload}
        >
          Odśwież obserwatorów
        </button>
        {params.has("watchers_after_id") && (
          <button
            className="button button--secondary"
            onClick={() => page("watchers_after_id")}
          >
            Pierwsza strona obserwatorów
          </button>
        )}
        {watchers.data?.next_after_id && (
          <button
            className="button button--secondary"
            onClick={() =>
              page("watchers_after_id", watchers.data!.next_after_id)
            }
          >
            Więcej obserwatorów
          </button>
        )}
      </div>
      <h3>Powiązane incydenty</h3>
      {access.data?.manage && (
        <form
          onSubmit={(event) => {
            event.preventDefault();
            if (target)
              void change("relations", "POST", {
                type: kind,
                target_id: target.id,
              });
          }}
        >
          <fieldset className="form-fields" disabled={busy}>
            <RemoteCombobox
              kind="users"
              label="Powiązany incydent"
              endpoint="/api/v1/incidents/lookups/incidents"
              value={target}
              onChange={setTarget}
              disabled={busy}
            />
            <label>
              Rodzaj relacji
              <select
                aria-label="Rodzaj relacji"
                value={kind}
                onChange={(event) => setKind(event.target.value)}
              >
                <option value="RELATED">Powiązany</option>
                <option value="PARENT">
                  Ten incydent jest nadrzędny dla wybranego
                </option>
              </select>
            </label>
          </fieldset>
          <button
            className="button button--primary"
            disabled={busy || !target || target.id === record.id}
          >
            Dodaj powiązanie
          </button>
        </form>
      )}
      {relations.loading ? (
        <PageState title="Ładowanie relacji…" />
      ) : relations.error ? (
        <PageState
          title="Błąd odczytu relacji"
          detail={relations.error}
          tone="error"
        />
      ) : !relations.data?.relations.length ? (
        <PageState title="Brak dostępnych powiązań" />
      ) : (
        <ul>
          {relations.data.relations.map((r) => (
            <li key={r.id}>
              <Link to={`/incidents/${r.other_id}`}>{r.number}</Link> ·{" "}
              {r.title} ·{" "}
              {r.type === "RELATED"
                ? "Powiązany"
                : r.source_id === record.id
                  ? "Podrzędny"
                  : "Nadrzędny"}
              {access.data?.manage && (
                <button
                  className="button button--secondary"
                  disabled={busy}
                  onClick={() => void change(`relations/${r.id}`, "DELETE")}
                >
                  Usuń powiązanie {r.number}
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      <div className="action-row">
        <button
          className="button button--secondary"
          disabled={busy}
          onClick={relations.reload}
        >
          Odśwież powiązania
        </button>
        {params.has("relations_before_id") && (
          <button
            className="button button--secondary"
            onClick={() => page("relations_before_id")}
          >
            Najnowsze powiązania
          </button>
        )}
        {relations.data?.next_before_id && (
          <button
            className="button button--secondary"
            onClick={() =>
              page("relations_before_id", relations.data!.next_before_id)
            }
          >
            Starsze powiązania
          </button>
        )}
      </div>
    </>
  );
}
