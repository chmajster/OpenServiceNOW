import { useEffect, useRef, useState, type FormEvent } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { api, ApiError, jsonBody } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { PageState } from "../components/PageState";
import { RemoteCombobox, type Choice } from "../components/RemoteCombobox";
import { useIncidentQuery, useIncidentTitleFocus } from "../incidents/hooks";
import { IncidentFields } from "../incidents/FieldsForm";
import {
  fieldsOf,
  optionLabel,
  dateLabel,
  type FieldDraft,
  type Incident,
  type IncidentPage,
  type Metadata,
} from "../incidents/contracts";
import { IncidentWorkspace } from "../incidents/IncidentWorkspace";

export function IncidentsPage() {
  const { session, hasPermission } = useAuth();
  if (!hasPermission("incident.read") && !hasPermission("incident.read_own"))
    return (
      <PageState
        title="Brak dostępu do incydentów"
        detail="Potrzebujesz uprawnienia do odczytu incydentów lub własnych zgłoszeń."
        tone="error"
      />
    );
  return <IncidentContents key={`${session?.tenant.id}:${session?.user.id}`} />;
}
function IncidentContents() {
  const { incidentID } = useParams();
  const metadata = useIncidentQuery<Metadata>("/api/v1/incidents/metadata");
  if (metadata.loading) return <PageState title="Ładowanie incydentów…" />;
  if (!metadata.data)
    return (
      <>
        <PageState
          title="Nie udało się odczytać konfiguracji incydentów"
          detail={metadata.error}
          tone="error"
        />
        <button className="button button--secondary" onClick={metadata.reload}>
          Spróbuj ponownie
        </button>
      </>
    );
  return incidentID === "new" ? (
    <NewIncident metadata={metadata.data} />
  ) : incidentID ? (
    <IncidentDetails
      key={incidentID}
      id={incidentID}
      metadata={metadata.data}
    />
  ) : (
    <IncidentList metadata={metadata.data} />
  );
}
function IncidentList({ metadata }: { metadata: Metadata }) {
  const heading = useIncidentTitleFocus();
  const [params, setParams] = useSearchParams();
  const [query, setQuery] = useState(params.get("q") ?? "");
  useEffect(() => setQuery(params.get("q") ?? ""), [params.get("q")]);
  const requestParams = new URLSearchParams(params);
  requestParams.delete("tab");
  const result = useIncidentQuery<IncidentPage>(
    `/api/v1/incidents/?${requestParams}`,
  );
  const view = params.get("view") ?? "all";
  function filter(values: Record<string, string>) {
    const next = new URLSearchParams(params);
    next.delete("after_id");
    next.delete("after_priority");
    for (const [key, value] of Object.entries(values)) {
      if (value) next.set(key, value);
      else next.delete(key);
    }
    setParams(next);
  }
  const choice = (id: string, label: string): Choice | null =>
    params.get(id)
      ? {
          id: params.get(id)!,
          label: params.get(label) ?? `#${params.get(id)}`,
        }
      : null;
  return (
    <section className="page incident-page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">Obsługa zgłoszeń</p>
          <h1 ref={heading} tabIndex={-1}>
            Incydenty
          </h1>
          <p className="muted">
            Wyniki są filtrowane i stronicowane na serwerze.
          </p>
        </div>
        {metadata.can_create && (
          <Link className="button button--primary" to="/incidents/new">
            Zgłoś incydent
          </Link>
        )}
      </div>
      <nav className="tabs" aria-label="Widoki incydentów">
        {[
          ["all", "Wszystkie dostępne"],
          ["mine", "Moje"],
          ["my_group", "Mojej grupy"],
          ["unassigned", "Nieprzypisane"],
        ].map(([value, label]) => (
          <Link
            key={value}
            className={view === value ? "is-active" : ""}
            aria-current={view === value ? "page" : undefined}
            to={`/incidents?${(() => {
              const p = new URLSearchParams(params);
              p.set("view", value!);
              p.delete("after_id");
              p.delete("after_priority");
              return p;
            })()}`}
          >
            {label}
          </Link>
        ))}
      </nav>
      <div className="panel">
        <form
          className="action-row"
          onSubmit={(e) => {
            e.preventDefault();
            filter({ q: query.trim() });
          }}
        >
          <label className="incident-search">
            Numer lub tytuł
            <input
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              maxLength={200}
            />
          </label>
          <button className="button button--secondary">Szukaj</button>
          <button
            type="button"
            className="button button--secondary"
            onClick={() => {
              setQuery("");
              setParams({ view });
            }}
          >
            Wyczyść filtry
          </button>
        </form>
        <div className="filter-grid">
          <label>
            Stan
            <select
              aria-label="Stan"
              value={params.get("state") ?? ""}
              onChange={(e) => filter({ state: e.target.value })}
            >
              <option value="">Wszystkie stany</option>
              {metadata.states.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          </label>
          <label>
            Priorytet
            <select
              aria-label="Priorytet"
              value={params.get("priority") ?? ""}
              onChange={(e) => filter({ priority: e.target.value })}
            >
              <option value="">Wszystkie priorytety</option>
              {[1, 2, 3, 4, 5].map((p) => (
                <option key={p} value={p}>
                  P{p}
                </option>
              ))}
            </select>
          </label>
          <label>
            Kategoria
            <select
              aria-label="Kategoria"
              value={params.get("category") ?? ""}
              onChange={(e) => filter({ category: e.target.value })}
            >
              <option value="">Wszystkie kategorie</option>
              {metadata.categories.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          </label>
          <label>
            Kolejność
            <select
              aria-label="Kolejność"
              value={params.get("sort") ?? "newest"}
              onChange={(e) => filter({ sort: e.target.value })}
            >
              <option value="newest">Najnowsze</option>
              <option value="oldest">Najstarsze</option>
              <option value="priority">Najwyższy priorytet</option>
            </select>
          </label>
          <label>
            Utworzone od (UTC)
            <input
              type="date"
              value={params.get("from")?.slice(0, 10) ?? ""}
              onChange={(e) =>
                filter({
                  from: e.target.value ? `${e.target.value}T00:00:00Z` : "",
                })
              }
            />
          </label>
          <label>
            Utworzone do (UTC)
            <input
              type="date"
              value={params.get("to")?.slice(0, 10) ?? ""}
              onChange={(e) =>
                filter({
                  to: e.target.value ? `${e.target.value}T23:59:59.999Z` : "",
                })
              }
            />
          </label>
        </div>
        <details>
          <summary>Osoby, grupa i liczba wyników</summary>
          <div className="filter-grid">
            <RemoteCombobox
              kind="users"
              label="Przypisany użytkownik"
              endpoint="/api/v1/incidents/lookups/users?purpose=affected"
              value={choice("assigned_to", "assigned_label")}
              onChange={(c) =>
                filter({
                  assigned_to: c?.id ?? "",
                  assigned_label: c?.label ?? "",
                })
              }
            />
            <RemoteCombobox
              kind="groups"
              label="Grupa przypisania"
              endpoint="/api/v1/incidents/lookups/groups"
              value={choice("group_id", "group_label")}
              onChange={(c) =>
                filter({ group_id: c?.id ?? "", group_label: c?.label ?? "" })
              }
            />
            <RemoteCombobox
              kind="users"
              label="Zgłaszający"
              endpoint="/api/v1/incidents/lookups/users?purpose=affected"
              value={choice("reporter_id", "reporter_label")}
              onChange={(c) =>
                filter({
                  reporter_id: c?.id ?? "",
                  reporter_label: c?.label ?? "",
                })
              }
            />
            <label>
              Wyników na stronie
              <select
                aria-label="Wyników na stronie"
                value={params.get("limit") ?? "25"}
                onChange={(e) => filter({ limit: e.target.value })}
              >
                {[10, 25, 50].map((n) => (
                  <option key={n} value={n}>
                    {n}
                  </option>
                ))}
              </select>
            </label>
          </div>
        </details>
      </div>
      <div className="panel">
        <div className="action-row">
          <h2>Lista incydentów</h2>
          <button
            className="button button--secondary"
            disabled={result.loading}
            onClick={result.reload}
          >
            Odśwież listę
          </button>
        </div>
        {result.loading ? (
          <PageState title="Ładowanie listy…" />
        ) : result.error ? (
          <PageState
            title="Nie udało się pobrać incydentów"
            detail={result.error}
            tone="error"
          />
        ) : !result.data?.incidents.length ? (
          <PageState title="Brak incydentów dla wybranych filtrów" />
        ) : (
          <div className="table-scroll">
            <table>
              <caption className="sr-only">
                Incydenty dostępne w bieżącej organizacji
              </caption>
              <thead>
                <tr>
                  <th scope="col">Numer i tytuł</th>
                  <th scope="col">Stan</th>
                  <th scope="col">Priorytet</th>
                  <th scope="col">Przypisanie</th>
                  <th scope="col">Zgłaszający</th>
                  <th scope="col">Utworzono</th>
                </tr>
              </thead>
              <tbody>
                {result.data.incidents.map((r) => (
                  <tr key={r.id}>
                    <td className="wrap-cell">
                      <Link
                        to={`/incidents/${r.id}`}
                        state={{ listSearch: params.toString() }}
                      >
                        <strong>{r.number}</strong>
                      </Link>
                      <div>{r.title}</div>
                    </td>
                    <td>{optionLabel(metadata.states, r.state)}</td>
                    <td>
                      <span
                        className={`incident-priority priority-${r.priority}`}
                      >
                        P{r.priority}
                      </span>
                    </td>
                    <td className="wrap-cell">
                      {r.assigned_name || "Nieprzypisany"}
                      <div className="muted">{r.group_name || "Bez grupy"}</div>
                    </td>
                    <td className="wrap-cell">{r.reporter_name}</td>
                    <td>{dateLabel(r.created_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <div className="action-row">
          {params.has("after_id") && (
            <button
              className="button button--secondary"
              disabled={result.loading}
              onClick={() => filter({})}
            >
              Pierwsza strona
            </button>
          )}
          {result.data?.next_after_id && (
            <button
              className="button button--secondary"
              disabled={result.loading}
              onClick={() => {
                const next = new URLSearchParams(params);
                next.set("after_id", result.data!.next_after_id!);
                if (result.data?.next_after_priority)
                  next.set("after_priority", result.data.next_after_priority);
                else next.delete("after_priority");
                setParams(next);
              }}
            >
              Następna strona
            </button>
          )}
        </div>
      </div>
    </section>
  );
}

function NewIncident({ metadata }: { metadata: Metadata }) {
  const { session } = useAuth();
  const [draft, setDraft] = useState<FieldDraft>(() => ({
    title: "",
    description: "",
    affected: session
      ? { id: session.user.id, label: session.user.display_name }
      : null,
    category: "other",
    subcategory: "other",
    impact: "3",
    urgency: "3",
  }));
  const [key] = useState(() => crypto.randomUUID());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [uncertain, setUncertain] = useState<ReturnType<typeof fieldsOf>>();
  const [created, setCreated] = useState<Incident>();
  const heading = useIncidentTitleFocus(created?.id);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  async function create(body: ReturnType<typeof fieldsOf>) {
    setBusy(true);
    setError(undefined);
    try {
      const result = await api<Incident>("/api/v1/incidents/", {
        method: "POST",
        headers: { "Idempotency-Key": key },
        body: jsonBody(body),
      });
      if (mounted.current) {
        setCreated(result);
        setUncertain(undefined);
      }
    } catch (cause) {
      if (!mounted.current) return;
      setError(
        cause instanceof Error
          ? cause.message
          : "Nie potwierdzono utworzenia incydentu.",
      );
      if (
        !(cause instanceof ApiError) ||
        cause.status >= 500 ||
        cause.status === 409
      )
        setUncertain(body);
    } finally {
      if (mounted.current) setBusy(false);
    }
  }
  function submit(event: FormEvent) {
    event.preventDefault();
    if (uncertain) return;
    try {
      void create(fieldsOf(draft));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Sprawdź dane.");
    }
  }
  if (!metadata.can_create)
    return (
      <PageState
        title="Nie masz uprawnienia do tworzenia incydentów"
        tone="error"
      />
    );
  if (created)
    return (
      <section className="page incident-page">
        <h1 ref={heading} tabIndex={-1}>
          Incydent zgłoszony
        </h1>
        <div className="panel">
          <p role="status">
            Utworzono {created.number}: {created.title}.
          </p>
          <div className="action-row">
            <Link
              className="button button--primary"
              to={`/incidents/${created.id}`}
            >
              Otwórz {created.number}
            </Link>
            <Link
              className="button button--secondary"
              to="/incidents?view=mine"
            >
              Moje incydenty
            </Link>
          </div>
        </div>
      </section>
    );
  return (
    <section className="page incident-page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">Incydenty</p>
          <h1 ref={heading} tabIndex={-1}>
            Zgłoś incydent
          </h1>
          <p>Zgłaszający: {session?.user.display_name}</p>
        </div>
        <Link className="button button--secondary" to="/incidents">
          Wróć do listy
        </Link>
      </div>
      <div className="panel">
        <form onSubmit={submit}>
          <IncidentFields
            draft={draft}
            change={setDraft}
            metadata={metadata}
            disabled={busy || !!uncertain}
          />
          {error && (
            <p role="alert" className="notice notice--error">
              {error}
            </p>
          )}
          {uncertain ? (
            <div className="notice">
              <p>
                Nie potwierdzono wyniku poprzedniej próby. Zachowano jej dane i
                klucz, aby sprawdzenie nie utworzyło duplikatu.
              </p>
              <button
                type="button"
                className="button button--primary"
                disabled={busy}
                onClick={() => void create(uncertain)}
              >
                Sprawdź wynik poprzedniej próby
              </button>
            </div>
          ) : (
            <button className="button button--primary" disabled={busy}>
              {busy ? "Zapisywanie…" : "Utwórz incydent"}
            </button>
          )}
        </form>
      </div>
    </section>
  );
}
function IncidentDetails({ id, metadata }: { id: string; metadata: Metadata }) {
  const result = useIncidentQuery<Incident>(`/api/v1/incidents/${id}`);
  if (result.loading) return <PageState title="Ładowanie incydentu…" />;
  if (!result.data)
    return (
      <>
        <PageState
          title="Nie udało się odczytać incydentu"
          detail={result.error}
          tone="error"
        />
        <button className="button button--secondary" onClick={result.reload}>
          Spróbuj ponownie
        </button>
        <Link to="/incidents">Wróć do listy</Link>
      </>
    );
  return <IncidentWorkspace initial={result.data} metadata={metadata} />;
}
