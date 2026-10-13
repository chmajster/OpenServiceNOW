import { useCallback, useEffect, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { RecordHeader } from "../components/RecordHeader";
import { RemoteCombobox, type Choice } from "../components/RemoteCombobox";
import { TaskBatch } from "../recordlists/TaskBatch";
import { ListToolbar, columnConfig } from "../recordlists/ListToolbar";
import { RecordTable, type RecordColumn } from "../recordlists/RecordTable";
import type { FilterField } from "../recordlists/contracts";
import { CommunicationPanel } from "../incidents/CommunicationPanel";
import { useIncidentQuery, useIncidentTitleFocus } from "../incidents/hooks";
import { UnsavedGuard } from "../incidents/UnsavedGuard";
import { LoadState } from "../requests/LoadState";
import { useEditor } from "../requests/useEditor";
import { states, type Request, type Task } from "../requests/contracts";
import {
  useWorkspace,
  useWorkspaceRecord,
} from "../workspace/WorkspaceContext";
import { RecordCIPanel } from "../cmdb/components";
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
  const [batchBusy, setBatchBusy] = useState(false);
  const [picked, setPicked] = useState<{
    scope: string;
    items: Record<string, { id: string; version: string }>;
  }>({ scope: "", items: {} });
  const selection = picked.scope === params.toString() ? picked.items : {};
  const select = (items: Record<string, { id: string; version: string }>) =>
    setPicked({ scope: params.toString(), items });
  const root = tasks ? "fulfillment" : "requests";
  const q = params.get("q") ?? "",
    state = params.get("state") ?? "",
    view = params.get("view") ?? (tasks ? "group" : "mine");
  const queryParams = new URLSearchParams(params);
  queryParams.set("view", view);
  queryParams.set("limit", params.get("limit") ?? "25");
  const query = useIncidentQuery<{
    items: (Request | Task)[];
    total: string;
    groups: { value: string; count: string }[];
    next_before_id?: string;
    next_before_value?: string;
  }>(`/api/v1/${root}/?${queryParams}`);
  const page = {
    ...query,
    items: query.data?.items ?? [],
    next: query.data?.next_before_id,
    retry: query.reload,
    more: () => {
      const next = new URLSearchParams(params);
      next.set("before_id", query.data!.next_before_id!);
      if (query.data?.next_before_value)
        next.set("before_value", query.data.next_before_value);
      set(next);
    },
  };
  const columns: RecordColumn<Request | Task>[] = [
    {
      key: "number",
      label: "Numer",
      width: 160,
      render: (r) => (
        <Link to={`/${root}/${r.id}`} state={{ listSearch: params.toString() }}>
          {r.number}
        </Link>
      ),
    },
    ...(tasks
      ? [
          {
            key: "title",
            label: "Opis zadania",
            width: 280,
            render: (r: Request | Task) => ("title" in r ? r.title : ""),
          },
          {
            key: "group_name",
            label: "Grupa",
            width: 170,
            render: (r: Request | Task) => ("title" in r ? r.group_name : ""),
          },
          {
            key: "assigned_name",
            label: "Osoba przypisana",
            width: 170,
            render: (r: Request | Task) =>
              "title" in r ? r.assigned_name || "Nieprzypisane" : "",
          },
        ]
      : [
          {
            key: "requester_name",
            label: "Zgłaszający",
            width: 220,
            render: (r: Request | Task) =>
              "requester_name" in r ? r.requester_name : "",
          },
          {
            key: "created_at",
            label: "Data złożenia",
            width: 180,
            render: (r: Request | Task) =>
              "created_at" in r
                ? new Date(r.created_at).toLocaleString("pl-PL")
                : "",
          },
        ]),
    { key: "state", label: "Stan", width: 150, render: (r) => states[r.state] },
    {
      key: "updated_at",
      label: "Aktualizacja",
      width: 180,
      render: (r) => new Date(r.updated_at).toLocaleString("pl-PL"),
    },
  ];
  const options = tasks
    ? ["OPEN", "IN_PROGRESS", "ON_HOLD", "COMPLETED", "CANCELLED"]
    : ["SUBMITTED", "IN_PROGRESS", "FULFILLED", "CLOSED", "CANCELLED"];
  const fields: FilterField[] = [
    { key: "number", label: "Numer", kind: "text" },
    {
      key: "state",
      label: "Stan",
      kind: "enum",
      options: options.map((v) => ({ value: v, label: states[v]! })),
    },
    { key: "active", label: "Aktywne", kind: "boolean" },
    { key: "updated_at", label: "Aktualizacja", kind: "time" },
    ...(tasks
      ? [
          { key: "title", label: "Opis zadania", kind: "text" as const },
          { key: "my_groups", label: "Moje grupy", kind: "boolean" as const },
          {
            key: "group_id",
            label: "Grupa przypisania",
            kind: "id" as const,
            nullable: true,
            endpoint: "/api/v1/fulfillment/lookups/groups",
          },
          {
            key: "assigned_to",
            label: "Osoba przypisana",
            kind: "id" as const,
            nullable: true,
            endpoint: "/api/v1/fulfillment/lookups/users",
          },
        ]
      : [
          { key: "created_at", label: "Data złożenia", kind: "time" as const },
          {
            key: "requester_id",
            label: "Zgłaszający",
            kind: "id" as const,
            endpoint: "/api/v1/requests/lookups/users",
          },
        ]),
  ];
  const setParam = (key: string, v: string) => {
    const p = new URLSearchParams(params);
    p.delete("before_id");
    p.delete("before_value");
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
                <option value="my_group">Moich grup</option>
                <option value="group">Wszystkie dostępne</option>
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
      {params.get("active") === "true" && (
        <p className="notice">
          Aktywne rekordy{" "}
          <button
            className="button button--small button--secondary"
            onClick={() => setParam("active", "false")}
          >
            Uwzględnij zakończone
          </button>
        </p>
      )}
      <ListToolbar
        key={root}
        domain={root}
        fields={fields}
        columns={columns}
        apiRoot={`/api/v1/${root}`}
        total={query.data?.total}
        counts={query.data?.groups}
        sorts={[
          { value: "newest", label: "Najnowsze" },
          { value: "oldest", label: "Najstarsze" },
          { value: "number", label: "Numer A–Z" },
          { value: "updated", label: "Ostatnia aktualizacja" },
        ]}
        groups={
          tasks
            ? [
                { value: "state", label: "Stan" },
                { value: "group_id", label: "Grupa" },
                { value: "assigned_to", label: "Osoba przypisana" },
              ]
            : [
                { value: "state", label: "Stan" },
                { value: "requester_id", label: "Zgłaszający" },
              ]
        }
      />
      <LoadState loading={page.loading} error={page.error} retry={page.retry} />
      {!page.loading && !page.error && !page.items.length ? (
        <p role="status">Brak wniosków lub zadań pasujących do filtrów.</p>
      ) : null}
      {tasks && (
        <TaskBatch
          selection={Object.values(selection)}
          clear={() => select({})}
          refresh={page.retry}
          busy={batchBusy || page.loading}
          setBusy={setBatchBusy}
        />
      )}
      <RecordTable
        stateLabel={(state) => states[state] ?? state}
        selectionHeader={
          tasks ? (
            <input
              type="checkbox"
              aria-label="Wybierz wszystkie zadania na stronie"
              disabled={batchBusy || page.loading}
              checked={
                page.items.length > 0 &&
                page.items.every((r) => !!selection[r.id])
              }
              onChange={(e) =>
                select(
                  e.target.checked
                    ? Object.fromEntries(
                        page.items.map((r) => [
                          r.id,
                          { id: r.id, version: r.version },
                        ]),
                      )
                    : {},
                )
              }
            />
          ) : undefined
        }
        selectionCell={
          tasks
            ? (r) => (
                <input
                  type="checkbox"
                  aria-label={`Wybierz ${r.number}`}
                  disabled={batchBusy || page.loading}
                  checked={!!selection[r.id]}
                  onChange={(e) => {
                    const next = { ...selection };
                    if (e.target.checked)
                      next[r.id] = { id: r.id, version: r.version };
                    else delete next[r.id];
                    select(next);
                  }}
                />
              )
            : undefined
        }
        records={page.items}
        columns={columns}
        selected={columnConfig(params, columns)}
        apiRoot={`/api/v1/${root}`}
        url={(r) => `/${root}/${r.id}`}
      />

      {params.has("before_id") && (
        <button
          className="button button--secondary"
          onClick={() => {
            const next = new URLSearchParams(params);
            next.delete("before_id");
            next.delete("before_value");
            set(next);
          }}
        >
          Pierwsza strona
        </button>
      )}
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
  const workspace = useWorkspace();
  useWorkspaceRecord(
    {
      number: r.number,
      title: "title" in r ? r.title : r.number,
      state: states[r.state] ?? r.state,
      type: tasks ? "Zadanie" : "Wniosek",
      assignment:
        "title" in r
          ? [r.group_name, r.assigned_name].filter(Boolean).join(" · ")
          : r.requester_name,
      links:
        "title" in r
          ? [
              {
                url: `/requests/${r.request_id}`,
                label: `Wniosek ${r.request_number}`,
              },
            ]
          : undefined,
    },
    r.id,
  );
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
  const [proposalVersion, setProposalVersion] = useState(initial.version);
  useEffect(() => {
    if (!state && !reason && !assignmentDirty) setProposalVersion(r.version);
  }, [r.version, state, reason, assignmentDirty]);
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
      version: proposalVersion,
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
      { version: proposalVersion, assigned_to: assigned?.id ?? null },
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
      { version: proposalVersion, assigned_to: session.user.id },
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
      <RecordHeader
        headingRef={heading}
        number={r.number}
        title={"title" in r ? `${r.number} · ${r.title}` : r.number}
        type={tasks ? "Zadanie realizacji" : "Wniosek"}
        state={states[r.state] ?? r.state}
        version={r.version}
        assignment={
          "title" in r
            ? [r.group_name, r.assigned_name].filter(Boolean).join(" · ")
            : r.requester_name
        }
        actions={
          <>
            <Link
              className="button button--secondary"
              to={
                workspace?.listURL(`/${tasks ? "fulfillment" : "requests"}`) ??
                `/${tasks ? "fulfillment" : "requests"}`
              }
            >
              Wróć do kolejki
            </Link>
            <button
              className="button button--secondary"
              disabled={editor.busy}
              onClick={() => void editor.save(path, undefined, "GET")}
            >
              Odśwież rekord
            </button>
          </>
        }
      />
      {!tasks ? <RequestProcesses requestID={requestID} /> : null}
      {tasks ? (
        <p>
          <Link to={`/requests/${requestID}`}>
            Wniosek {"title" in r ? r.request_number : ""}
          </Link>
        </p>
      ) : null}
      <div className="tabs" role="tablist" aria-label="Szczegóły wniosku">
        {(tasks
          ? ["info", "actions", "communication", "history"]
          : ["info", "actions", "communication", "cmdb", "history"]
        ).map((t) => (
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
              const tabs = tasks
                ? ["info", "actions", "communication", "history"]
                : ["info", "actions", "communication", "cmdb", "history"];
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
                cmdb: "Elementy konfiguracji",
                history: "Historia",
              }[t as "info"]
            }
          </button>
        ))}
      </div>
      {tab === "cmdb" && !tasks && (
        <RecordCIPanel
          kind="requests"
          id={r.id}
          onChanged={() =>
            editor.save(path, undefined, "GET").then(() => undefined)
          }
        />
      )}
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
          <button
            className="button button--secondary"
            onClick={() => {
              setProposalVersion(editor.conflict!.version);
              editor.resolve();
            }}
          >
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
