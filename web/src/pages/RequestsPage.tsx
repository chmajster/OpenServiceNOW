import { useCallback, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { RemoteCombobox, type Choice } from "../components/RemoteCombobox";
import { useRemotePage } from "../components/useRemotePage";
import { CommunicationPanel } from "../incidents/CommunicationPanel";
import { useIncidentQuery, useIncidentTitleFocus } from "../incidents/hooks";
import { UnsavedGuard } from "../incidents/UnsavedGuard";
import { LoadState } from "../requests/LoadState";
import { useEditor } from "../requests/useEditor";
import { states, type Request, type Task } from "../requests/contracts";
import { RequestProcesses } from "../workflow/ProcessView";
export function RequestsPage({ tasks = false }: { tasks?: boolean }) {
  const { recordID } = useParams();
  return recordID ? (
    <Details key={`${tasks}:${recordID}`} id={recordID} tasks={tasks} />
  ) : (
    <Queue tasks={tasks} />
  );
}
function Queue({ tasks }: { tasks: boolean }) {
  const heading = useIncidentTitleFocus(String(tasks));
  const { hasPermission } = useAuth();
  const [params, set] = useSearchParams();
  const root = tasks ? "fulfillment" : "requests";
  const q = params.get("q") ?? "",
    state = params.get("state") ?? "",
    view = params.get("view") ?? (tasks ? "group" : "mine");
  const page = useRemotePage<Request | Task>(
    `/api/v1/${root}/?q=${encodeURIComponent(q)}&state=${state}&view=${view}&limit=25`,
    "items",
    "before_id",
  );
  const options = tasks
    ? ["OPEN", "IN_PROGRESS", "ON_HOLD", "COMPLETED", "CANCELLED"]
    : ["SUBMITTED", "IN_PROGRESS", "FULFILLED", "CLOSED", "CANCELLED"];
  const setParam = (key: string, v: string) => {
    const p = new URLSearchParams(params);
    p.set(key, v);
    set(p, { replace: true });
  };
  if (
    tasks &&
    !hasPermission("request.fulfill") &&
    !hasPermission("request.manage")
  )
    return <LoadState error="Brak uprawnień do realizacji." />;
  return (
    <>
      <h1 ref={heading} tabIndex={-1}>
        {tasks ? "Kolejka realizacji" : "Moje wnioski"}
      </h1>
      {!tasks ? <Link to="/catalog">Złóż wniosek z katalogu</Link> : null}
      <div className="panel form-grid">
        <label>
          Wyszukaj numer lub zadanie
          <input
            value={q}
            maxLength={200}
            onChange={(e) => setParam("q", e.target.value)}
          />
        </label>
        <label>
          Stan
          <select
            value={state}
            onChange={(e) => setParam("state", e.target.value)}
          >
            <option value="">Wszystkie</option>
            {options.map((s) => (
              <option value={s} key={s}>
                {states[s]}
              </option>
            ))}
          </select>
        </label>
        <label>
          Widok
          <select
            value={view}
            onChange={(e) => setParam("view", e.target.value)}
          >
            {tasks ? (
              <>
                <option value="group">Moich grup</option>
                <option value="mine">Przypisane do mnie</option>
                <option value="unassigned">Nieprzypisane</option>
              </>
            ) : (
              <>
                <option value="mine">Moje</option>
                {hasPermission("request.manage") ||
                hasPermission("request.fulfill") ? (
                  <option value="all">Dostępne dla mnie</option>
                ) : null}
              </>
            )}
          </select>
        </label>
      </div>
      <LoadState loading={page.loading} error={page.error} retry={page.retry} />
      {!page.loading && !page.error && !page.items.length ? (
        <p role="status">Brak wniosków lub zadań pasujących do filtrów.</p>
      ) : null}
      <div className="table-scroll">
        <table>
          <thead>
            <tr>
              <th>Numer</th>
              <th>{tasks ? "Zadanie" : "Zgłaszający"}</th>
              <th>Stan</th>
              <th>{tasks ? "Przypisanie" : "Data złożenia"}</th>
            </tr>
          </thead>
          <tbody>
            {page.items.map((r) => (
              <tr key={r.id}>
                <td>
                  <Link
                    to={`/${root}/${r.id}`}
                    state={{ listSearch: params.toString() }}
                  >
                    {r.number}
                  </Link>
                </td>
                <td>{"title" in r ? r.title : r.requester_name}</td>
                <td>{states[r.state]}</td>
                <td>
                  {"title" in r ? (
                    <>
                      {r.group_name}
                      <br />
                      {r.assigned_name || "Nieprzypisane"}
                    </>
                  ) : (
                    new Date(r.created_at).toLocaleString("pl-PL")
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {page.next ? (
        <button
          className="button button--secondary"
          disabled={page.loading}
          onClick={() => void page.more()}
        >
          Więcej wyników
        </button>
      ) : null}
    </>
  );
}
function Details({ id, tasks }: { id: string; tasks: boolean }) {
  const path = `/api/v1/${tasks ? "fulfillment" : "requests"}/${id}`;
  const query = useIncidentQuery<Request | Task>(path);
  return (
    <>
      <LoadState
        loading={query.loading}
        error={query.error}
        retry={query.reload}
      />
      {query.data ? (
        <Workspace key={id} initial={query.data} path={path} tasks={tasks} />
      ) : null}
    </>
  );
}
function Workspace({
  initial,
  path,
  tasks,
}: {
  initial: Request | Task;
  path: string;
  tasks: boolean;
}) {
  const { session } = useAuth();
  const editor = useEditor(initial, path);
  const r = editor.record;
  const heading = useIncidentTitleFocus(r.id);
  const [params, setParams] = useSearchParams();
  const selectedTab = params.get("tab");
  const tab =
    selectedTab &&
    ["info", "actions", "communication", "history"].includes(selectedTab)
      ? selectedTab
      : "info";
  const [state, setState] = useState(""),
    [reason, setReason] = useState(""),
    [assigned, setAssigned] = useState<Choice | null>(() =>
      "title" in r && r.assignment.assigned_to
        ? { id: r.assignment.assigned_to, label: r.assigned_name }
        : null,
    ),
    [assignmentDirty, setAssignmentDirty] = useState(false),
    [draftTabs, setDraftTabs] = useState<Record<string, boolean>>({});
  const retain = useCallback(
    (key: string, needed: boolean) =>
      setDraftTabs((p) => (p[key] === needed ? p : { ...p, [key]: needed })),
    [],
  );
  const requestID = "title" in r ? r.request_id : r.id;
  const request = useIncidentQuery<Request>(
    tasks ? `/api/v1/requests/${requestID}` : null,
  );
  const history = useIncidentQuery<{
    entries: {
      id: string;
      summary: string;
      type: string;
      actor_name: string;
      event_time: string;
      data: Record<string, unknown>;
    }[];
    next_before_id?: string;
  }>(
    tab === "history"
      ? `/api/v1/requests/${requestID}/history?limit=20&before_id=${params.get("history_before_id") ?? ""}`
      : null,
  );
  const changeTab = (v: string) => {
    const p = new URLSearchParams(params);
    p.set("tab", v);
    setParams(p);
  };
  async function transition() {
    const success = await editor.save(`${path}/transition`, {
      version: r.version,
      state,
      reason,
    });
    if (success) {
      setReason("");
      setState("");
      request.reload();
    }
  }
  async function assign() {
    if (!("title" in r)) return;
    const success = await editor.save(
      `${path}/assignment`,
      { version: r.version, assigned_to: assigned?.id ?? null },
      "PUT",
    );
    if (success) setAssignmentDirty(false);
  }
  async function assignSelf() {
    if (!session || !("title" in r)) return;
    setAssigned({ id: session.user.id, label: session.user.display_name });
    setAssignmentDirty(true);
    const success = await editor.save(
      `${path}/assignment`,
      { version: r.version, assigned_to: session.user.id },
      "PUT",
    );
    if (success) setAssignmentDirty(false);
  }
  return (
    <>
      <UnsavedGuard
        dirty={
          !!reason ||
          !!state ||
          assignmentDirty ||
          Object.values(draftTabs).some(Boolean)
        }
      />
      <h1 ref={heading} tabIndex={-1}>
        {r.number}
        {"title" in r ? ` · ${r.title}` : ""}
      </h1>
      <p>
        {states[r.state]} · Wersja {r.version}
      </p>
      <Link to={`/${tasks ? "fulfillment" : "requests"}`}>Wróć do kolejki</Link>
      {!tasks ? <RequestProcesses requestID={requestID} /> : null}
      {tasks ? (
        <p>
          <Link to={`/requests/${requestID}`}>
            Wniosek {"title" in r ? r.request_number : ""}
          </Link>
        </p>
      ) : null}
      <div className="tabs" role="tablist" aria-label="Szczegóły wniosku">
        {["info", "actions", "communication", "history"].map((t) => (
          <button
            key={t}
            role="tab"
            aria-selected={tab === t}
            id={`request-tab-${t}`}
            aria-controls={`request-panel-${t}`}
            tabIndex={tab === t ? 0 : -1}
            className="button button--secondary"
            onClick={() => changeTab(t)}
            onKeyDown={(event) => {
              const tabs = ["info", "actions", "communication", "history"];
              const index = tabs.indexOf(t);
              const next =
                event.key === "ArrowRight"
                  ? (index + 1) % tabs.length
                  : event.key === "ArrowLeft"
                    ? (index + tabs.length - 1) % tabs.length
                    : event.key === "Home"
                      ? 0
                      : event.key === "End"
                        ? tabs.length - 1
                        : -1;
              if (next < 0) return;
              event.preventDefault();
              const target = tabs[next];
              if (!target) return;
              changeTab(target);
              document.getElementById(`request-tab-${target}`)?.focus();
            }}
          >
            {
              {
                info: "Informacje",
                actions: "Realizacja",
                communication: "Komunikacja i pliki",
                history: "Historia",
              }[t as "info"]
            }
          </button>
        ))}
      </div>
      {editor.notice ? <p role="status">{editor.notice}</p> : null}
      {editor.error ? <p role="alert">{editor.error}</p> : null}
      {editor.conflict ? (
        <section className="panel conflict-panel">
          <h2>Konflikt wersji</h2>
          <p>
            Twoja propozycja: {states[state] ?? state}; {reason};{" "}
            {assigned?.label}. Aktualny stan: {states[editor.conflict.state]},
            wersja {editor.conflict.version}.
          </p>
          <details>
            <summary>Aktualny rekord</summary>
            <pre>{JSON.stringify(editor.conflict, null, 2)}</pre>
          </details>
          <button className="button button--secondary" onClick={editor.resolve}>
            Zachowaj propozycję i użyj aktualnej wersji
          </button>
        </section>
      ) : null}
      <div
        hidden={tab !== "info"}
        className="panel"
        role="tabpanel"
        id="request-panel-info"
        aria-labelledby="request-tab-info"
      >
        {"title" in r ? (
          <>
            <h2>Instrukcja realizacji</h2>
            <p className="preserve-lines">
              {r.instructions || "Wykonaj zadanie i zapisz wynik realizacji."}
            </p>
            <p>
              Grupa: {r.group_name} · {r.assigned_name || "Nieprzypisane"} ·{" "}
              {r.required ? "Zadanie wymagane" : "Zadanie opcjonalne"}
            </p>
            <h3>Ostatni wynik / powód wstrzymania</h3>
            <p className="preserve-lines">{r.result || "Brak"}</p>
            <LoadState
              loading={request.loading}
              error={request.error}
              retry={request.reload}
            />
            {request.data ? <Items record={request.data} /> : null}
          </>
        ) : (
          <>
            <p>Zgłaszający: {r.requester_name}</p>
            <p>Złożono: {new Date(r.created_at).toLocaleString("pl-PL")}</p>
            {r.reason ? <p>Powód: {r.reason}</p> : null}
            <Items record={r} />
          </>
        )}
      </div>
      <div
        hidden={tab !== "actions"}
        className="panel"
        role="tabpanel"
        id="request-panel-actions"
        aria-labelledby="request-tab-actions"
      >
        {"title" in r && r.state !== "COMPLETED" && r.state !== "CANCELLED" ? (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              void assign();
            }}
          >
            <h2>Przypisanie zadania</h2>
            <RemoteCombobox
              kind="users"
              label="Realizujący"
              value={assigned}
              onChange={(c) => {
                setAssigned(c);
                setAssignmentDirty(true);
              }}
              endpoint={`${path}/users`}
              disabled={editor.busy || !!editor.conflict}
            />
            <button
              type="button"
              className="button button--secondary"
              disabled={editor.busy || !!editor.conflict || !session}
              onClick={() => void assignSelf()}
            >
              Przypisz do siebie
            </button>
            <button
              className="button button--primary"
              disabled={editor.busy || !!editor.conflict || !assignmentDirty}
            >
              Zapisz przypisanie
            </button>
          </form>
        ) : null}
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void transition();
          }}
        >
          <h2>Zmiana stanu</h2>
          {state === "FULFILLED" ? (
            <p>
              Wszystkie wymagane zadania muszą być ukończone. Pozostałe
              opcjonalne zadania zostaną anulowane.
            </p>
          ) : null}
          {!r.actions.length ? (
            <p>
              Brak dozwolonych przejść. Wymagane zadania muszą zostać ukończone
              przed zamknięciem wniosku.
            </p>
          ) : (
            <>
              <label>
                Nowy stan
                <select
                  aria-label="Nowy stan"
                  required
                  value={state}
                  disabled={editor.busy || !!editor.conflict}
                  onChange={(e) => setState(e.target.value)}
                >
                  <option value="">Wybierz…</option>
                  {r.actions.map((s) => (
                    <option key={s} value={s}>
                      {states[s]}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Opis wyniku lub uzasadnienie
                <textarea
                  value={reason}
                  maxLength={10000}
                  required={["CANCELLED", "COMPLETED", "ON_HOLD"].includes(
                    state,
                  )}
                  disabled={editor.busy || !!editor.conflict}
                  onChange={(e) => setReason(e.target.value)}
                />
              </label>
              <button
                className="button button--primary"
                disabled={editor.busy || !!editor.conflict || !state}
              >
                Zapisz stan
              </button>
            </>
          )}
        </form>
      </div>
      <div
        hidden={tab !== "communication"}
        role="tabpanel"
        id="request-panel-communication"
        aria-labelledby="request-tab-communication"
      >
        <CommunicationPanel
          record={{ id: requestID }}
          apiBase={`/api/v1/requests/${requestID}`}
          retain={retain}
        />
      </div>
      <div
        hidden={tab !== "history"}
        className="panel"
        role="tabpanel"
        id="request-panel-history"
        aria-labelledby="request-tab-history"
      >
        <h2>Historia</h2>
        <p>Worker aktualizuje historię asynchronicznie.</p>
        <button className="button button--secondary" onClick={history.reload}>
          Odśwież historię
        </button>
        <LoadState
          loading={history.loading}
          error={history.error}
          retry={history.reload}
        />
        {history.data?.entries.map((e) => (
          <article key={e.id}>
            <strong>{e.summary}</strong>
            <p>
              {e.actor_name} · {new Date(e.event_time).toLocaleString("pl-PL")}
            </p>
            {e.data.state ? (
              <p>
                Stan: {states[String(e.data.state)] ?? String(e.data.state)}
              </p>
            ) : null}
            <details>
              <summary>Szczegóły zdarzenia</summary>
              <pre>{JSON.stringify(e.data, null, 2)}</pre>
            </details>
          </article>
        ))}
        {history.data?.next_before_id ? (
          <button
            className="button button--secondary"
            onClick={() => {
              const p = new URLSearchParams(params);
              p.set("history_before_id", history.data!.next_before_id!);
              setParams(p);
            }}
          >
            Starsze zdarzenia
          </button>
        ) : null}
      </div>
    </>
  );
}
function Items({ record }: { record: Request }) {
  return (
    <>
      {record.items.map((i) => (
        <section key={i.id}>
          <h2>
            {i.number} · {i.snapshot.title}
          </h2>
          <p>
            Wersja zamówionej usługi: {i.snapshot.version}. Ukończono{" "}
            {i.completed_tasks}/{i.total_tasks} zadań; wymagane{" "}
            {i.required_completed}/{i.required_tasks}.
          </p>
          <dl>
            {i.snapshot.fields
              .filter((f) => Object.hasOwn(i.values, f.key))
              .map((f) => (
                <div key={f.key}>
                  <dt>{f.label}</dt>
                  <dd className="preserve-lines">
                    {typeof i.values[f.key] === "boolean"
                      ? i.values[f.key]
                        ? "Tak"
                        : "Nie"
                      : String(i.values[f.key])}
                  </dd>
                </div>
              ))}
          </dl>
        </section>
      ))}
    </>
  );
}
