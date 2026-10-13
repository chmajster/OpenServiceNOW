import { ConflictComparison } from "../components/ConflictComparison";
import { useCallback, useEffect, useRef, useState } from "react";
import {
  Link,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { RecordHeader } from "../components/RecordHeader";
import { PageState } from "../components/PageState";
import { useIncidentQuery } from "../incidents/hooks";
import { UnsavedGuard } from "../incidents/UnsavedGuard";
import { CommunicationPanel } from "../incidents/CommunicationPanel";
import { InventoryHistory, RecordCIPanel } from "../cmdb/components";
import { useCreation } from "../requests/useCreation";
import { useEditor } from "../requests/useEditor";
import {
  useWorkspace,
  useWorkspaceRecord,
} from "../workspace/WorkspaceContext";
import { ListToolbar } from "../recordlists/ListToolbar";
import { RecordTable, type RecordColumn } from "../recordlists/RecordTable";
import { columnsFrom, type FilterField } from "../recordlists/contracts";
import { ProblemChanges } from "../changes/ProblemChanges";
import { ProblemForm } from "../problems/ProblemForm";
import { ProblemIncidents, ProblemTasks } from "../problems/panels";
import {
  blank,
  fields,
  labels,
  states,
  type Problem,
  type ProblemFields,
  type SourceIncident,
} from "../problems/contracts";

const options = (keys: string[]) =>
  keys.map((value) => ({ value, label: labels[value] ?? value }));
export function ProblemsPage() {
  const { problemID } = useParams(),
    { hasPermission } = useAuth();
  const p = useIncidentQuery<Problem>(
    problemID && problemID !== "new" ? `/api/v1/problems/${problemID}` : null,
  );
  if (!hasPermission("problem.read") && !hasPermission("problem.read_own"))
    return <PageState title="Brak uprawnień do problemów" />;
  if (!problemID) return <ProblemList />;
  if (problemID === "new") return <CreateProblem />;
  if (p.loading) return <PageState title="Ładowanie problemu…" />;
  if (p.error || !p.data)
    return (
      <PageState
        title="Nie udało się otworzyć problemu"
        tone="error"
        detail={p.error ?? undefined}
        action={<button onClick={p.reload}>Spróbuj ponownie</button>}
      />
    );
  return <Editor key={problemID} initial={p.data} />;
}
function ProblemList() {
  const [params, setParams] = useSearchParams(),
    { hasPermission } = useAuth();
  const q = new URLSearchParams(params);
  q.delete("columns");
  if (!q.has("limit")) q.set("limit", "25");
  const p = useIncidentQuery<{
    items: Problem[];
    total: string;
    groups: { value: string; count: string }[];
    next_before_id?: string;
    next_before_value?: string;
  }>(`/api/v1/problems/?${q}`);
  const [search, setSearch] = useState(params.get("q") ?? "");
  useEffect(() => setSearch(params.get("q") ?? ""), [params]);
  const cols: RecordColumn<Problem>[] = [
    {
      key: "number",
      label: "Numer",
      width: 160,
      render: (r) => <Link to={`/problems/${r.id}`}>{r.number}</Link>,
    },
    {
      key: "title",
      label: "Opis skrócony",
      width: 270,
      render: (r) => r.title,
    },
    { key: "state", label: "Stan", width: 170, render: (r) => labels[r.state] },
    {
      key: "impact",
      label: "Wpływ",
      width: 100,
      render: (r) => labels[r.impact],
    },
    {
      key: "reporter",
      label: "Zgłaszający",
      width: 150,
      render: (r) => r.reporter_name,
    },
    {
      key: "assigned",
      label: "Analityk",
      width: 150,
      render: (r) => r.assigned_name || "Nieprzypisany",
    },
    {
      key: "group",
      label: "Grupa",
      width: 170,
      render: (r) => r.group_name || "Nieprzypisana",
    },
    {
      key: "known_error",
      label: "Known Error",
      width: 100,
      render: (r) => (r.known_error ? "Tak" : "Nie"),
    },
    {
      key: "updated_at",
      label: "Aktualizacja",
      width: 170,
      render: (r) => new Date(r.updated_at).toLocaleString("pl-PL"),
    },
  ];
  const filters: FilterField[] = [
    { key: "number", label: "Numer", kind: "text" },
    { key: "title", label: "Opis skrócony", kind: "text" },
    { key: "state", label: "Stan", kind: "enum", options: options(states) },
    {
      key: "impact",
      label: "Wpływ",
      kind: "enum",
      options: options(["HIGH", "MEDIUM", "LOW"]),
    },
    { key: "active", label: "Aktywne", kind: "boolean" },
    { key: "known_error", label: "Known Error", kind: "boolean" },
    {
      key: "assignment_group_id",
      label: "Grupa",
      kind: "id",
      nullable: true,
      endpoint: "/api/v1/problems/lookups/groups",
      collection: "items",
    },
    {
      key: "assigned_to",
      label: "Analityk",
      kind: "id",
      nullable: true,
      endpoint: "/api/v1/problems/lookups/users",
      collection: "items",
    },
  ];
  function first(n: URLSearchParams) {
    n.delete("before_id");
    n.delete("before_value");
    setParams(n);
  }
  return (
    <>
      <header className="page-heading">
        <div>
          <p className="eyebrow">Problem Management</p>
          <h1>Problemy</h1>
        </div>
        {hasPermission("problem.create") && (
          <Link className="button button--primary" to="/problems/new">
            Nowy problem
          </Link>
        )}
      </header>
      <form
        className="filter-grid"
        onSubmit={(e) => {
          e.preventDefault();
          const n = new URLSearchParams(params);
          search.trim() ? n.set("q", search.trim()) : n.delete("q");
          first(n);
        }}
      >
        <label>
          Wyszukaj problem
          <input
            value={search}
            maxLength={160}
            onChange={(e) => setSearch(e.target.value)}
          />
        </label>
        <label>
          Kolejka
          <select
            aria-label="Kolejka"
            value={params.get("view") ?? "all"}
            onChange={(e) => {
              const n = new URLSearchParams(params);
              n.set("view", e.target.value);
              first(n);
            }}
          >
            <option value="all">Wszystkie dostępne</option>
            <option value="mine">Przypisane do mnie</option>
            <option value="my_group">Moje grupy</option>
            <option value="unassigned">Bez wykonawcy</option>
          </select>
        </label>
        <button className="button">Zastosuj wyszukiwanie</button>
        <button type="button" className="button" onClick={p.reload}>
          Odśwież listę
        </button>
      </form>
      <ListToolbar
        domain="problems"
        fields={filters}
        columns={cols}
        sorts={options(["newest", "oldest", "title", "updated"])}
        groups={filters
          .filter((f) =>
            ["state", "impact", "assignment_group_id", "known_error"].includes(
              f.key,
            ),
          )
          .map((f) => ({ value: f.key, label: f.label }))}
        apiRoot="/api/v1/problems"
        total={p.data?.total}
        counts={p.data?.groups}
      />
      {p.loading ? (
        <PageState title="Ładowanie problemów…" />
      ) : p.error ? (
        <PageState
          title="Błąd listy problemów"
          tone="error"
          detail={p.error}
          action={<button onClick={p.reload}>Spróbuj ponownie</button>}
        />
      ) : !p.data?.items.length ? (
        <PageState title="Brak problemów dla wybranych filtrów" />
      ) : (
        <RecordTable
          records={p.data.items}
          columns={cols}
          selected={columnsFrom(params.get("columns"), cols)}
          apiRoot="/api/v1/problems"
          url={(r) => `/problems/${r.id}`}
          stateLabel={(s) => labels[s] ?? s}
        />
      )}
      <div className="action-row">
        {params.has("before_id") && (
          <button onClick={() => first(new URLSearchParams(params))}>
            Pierwsza strona
          </button>
        )}
        {p.data?.next_before_id && (
          <button
            onClick={() => {
              const n = new URLSearchParams(params);
              n.set("before_id", p.data!.next_before_id!);
              if (p.data!.next_before_value)
                n.set("before_value", p.data!.next_before_value!);
              setParams(n);
            }}
          >
            Następna strona
          </button>
        )}
      </div>
    </>
  );
}
function CreateProblem() {
  const { hasPermission } = useAuth(),
    [params] = useSearchParams(),
    navigate = useNavigate(),
    creation = useCreation<Problem>();
  const sourceID = params.get("source_incident_id"),
    source = useIncidentQuery<SourceIncident>(
      sourceID ? `/api/v1/problems/source-incidents/${sourceID}` : null,
    );
  const [draft, setDraft] = useState<ProblemFields>({ ...blank }),
    [copied, setCopied] = useState(false),
    [saved, setSaved] = useState("");
  useEffect(() => {
    if (saved) navigate(saved, { replace: true });
  }, [saved, navigate]);
  if (!hasPermission("problem.create"))
    return <PageState title="Brak uprawnień do rejestracji problemu" />;
  return (
    <>
      <UnsavedGuard
        dirty={
          !saved &&
          (JSON.stringify(draft) !== JSON.stringify(blank) ||
            creation.uncertain)
        }
      />
      <RecordHeader
        number="Nowy PRB"
        title="Rejestracja problemu"
        type="Problem"
        state="Rejestracja"
        version="—"
        actions={
          <Link className="button" to="/problems">
            Wróć do listy
          </Link>
        }
      />
      {sourceID && (
        <section className="panel">
          <h2>Podgląd informacji z incydentu</h2>
          {source.loading ? (
            <PageState title="Ładowanie podglądu…" />
          ) : source.error ? (
            <PageState
              title="Brak dostępnego incydentu"
              tone="error"
              detail={source.error}
              action={<button onClick={source.reload}>Ponów odczyt</button>}
            />
          ) : (
            source.data && (
              <>
                <p>
                  {source.data.number} · {source.data.title} · Wpływ:{" "}
                  {labels[source.data.impact]}
                </p>
                <p className="preserve-lines">{source.data.description}</p>
                <p>
                  Kopiowane pola: opis skrócony, opis i wpływ. Notatki i pliki
                  nie są przenoszone.
                </p>
                <button
                  disabled={
                    creation.busy ||
                    creation.uncertain ||
                    !source.data.can_link ||
                    copied
                  }
                  onClick={() => {
                    setDraft((d) => ({
                      ...d,
                      title: source.data!.title,
                      description: source.data!.description,
                      impact: source.data!.impact,
                    }));
                    setCopied(true);
                  }}
                >
                  Użyj pokazanych informacji i powiąż incydent
                </button>
              </>
            )
          )}
        </section>
      )}
      <form
        onSubmit={async (e) => {
          e.preventDefault();
          const r = await creation.create("/api/v1/problems/", {
            ...draft,
            ...(source.data && copied
              ? {
                  source_incident_id: source.data.id,
                  source_version: source.data.version,
                }
              : {}),
          });
          if (r) setSaved(`/problems/${r.id}`);
        }}
      >
        <ProblemForm
          draft={draft}
          change={setDraft}
          disabled={creation.busy || creation.uncertain}
          canAssign={hasPermission("problem.assign")}
          canKnownError={hasPermission("problem.known_error")}
        />
        {creation.error && <p role="alert">{creation.error}</p>}
        {creation.uncertain && (
          <p role="status">
            Nie potwierdzono zapisu. Ponów identyczną operację, aby sprawdzić
            wynik bez tworzenia duplikatu.
          </p>
        )}
        <button
          className="button button--primary"
          disabled={
            creation.busy ||
            (!creation.uncertain &&
              (!draft.title.trim() ||
                !draft.description.trim() ||
                (!!sourceID && (!copied || !source.data?.can_link))))
          }
        >
          {creation.uncertain ? "Potwierdź wynik zapisu" : "Utwórz problem"}
        </button>
      </form>
    </>
  );
}
function Editor({ initial }: { initial: Problem }) {
  const path = `/api/v1/problems/${initial.id}`,
    editor = useEditor(initial, path),
    r = editor.record,
    workspace = useWorkspace(),
    { hasPermission } = useAuth();
  const [base, setBase] = useState(initial),
    [draft, setDraft] = useState(fields(initial)),
    [reason, setReason] = useState(""),
    [target, setTarget] = useState("");
  const [params, setParams] = useSearchParams(),
    [retained, setRetained] = useState<Record<string, boolean>>({});
  const retain = useCallback(
    (tab: string, needed: boolean) =>
      setRetained((v) => (v[tab] === needed ? v : { ...v, [tab]: needed })),
    [],
  );
  const dirty = JSON.stringify(draft) !== JSON.stringify(fields(base)),
    accept = useRef(false),
    operation = useRef<{ body: string; key: string } | undefined>(undefined);
  useEffect(() => {
    if (r.version !== base.version && (accept.current || !dirty)) {
      accept.current = false;
      setBase(r);
      setDraft(fields(r));
    }
  }, [r, base.version, dirty]);
  useWorkspaceRecord(
    {
      number: r.number,
      title: r.title,
      state: labels[r.state] ?? r.state,
      type: "Problem",
      assignment: [r.group_name, r.assigned_name].filter(Boolean).join(" · "),
      links: [
        { url: `/problems/${r.id}?tab=related`, label: "Incydenty i CI" },
        { url: `/problems/${r.id}?tab=tasks`, label: "Zadania analityczne" },
      ],
    },
    r.id,
  );
  async function save(
    url: string,
    body: unknown,
    method = "POST",
    replace = false,
  ) {
    const fingerprint = method + url + JSON.stringify(body);
    if (
      !operation.current ||
      (operation.current.body !== fingerprint && !editor.uncertain)
    )
      operation.current = { body: fingerprint, key: crypto.randomUUID() };
    accept.current = replace;
    const ok = await editor.save(url, body, method, {
      "Idempotency-Key": operation.current.key,
    });
    if (ok) operation.current = undefined;
    else accept.current = false;
    return ok;
  }
  const tabs = [
    ["details", "Szczegóły"],
    ["activity", "Aktywność"],
    ["related", "Powiązane rekordy"],
    ["tasks", "Zadania"],
    ["history", "Historia"],
  ];
  const tab = tabs.some(([v]) => v === params.get("tab"))
    ? params.get("tab")!
    : "details";
  function choose(key: string) {
    const n = new URLSearchParams(params);
    n.set("tab", key);
    setParams(n);
  }
  const relatedPending = !!(
    retained.related_cis ||
    retained.related_incidents ||
    retained.related_problems
  );
  const blocked =
    editor.busy || !!editor.conflict || editor.uncertain || relatedPending;
  return (
    <>
      <UnsavedGuard
        searchKeys={retained.tasks ? ["task_id"] : []}
        dirty={
          dirty ||
          !!reason ||
          editor.uncertain ||
          Object.values(retained).some(Boolean)
        }
      />
      <RecordHeader
        number={r.number}
        title={r.title}
        type="Problem"
        state={labels[r.state] ?? r.state}
        version={r.version}
        severity={
          <span>
            Wpływ: {labels[r.impact]}
            {r.known_error ? " · Known Error" : ""}
          </span>
        }
        assignment={[r.group_name, r.assigned_name].filter(Boolean).join(" · ")}
        actions={
          <>
            <Link
              className="button"
              to={workspace?.listURL("/problems") ?? "/problems"}
            >
              Wróć do listy
            </Link>
            <button
              disabled={blocked}
              onClick={() => void save(path, undefined, "GET")}
            >
              Odśwież problem
            </button>
          </>
        }
      />
      {editor.error && <p role="alert">{editor.error}</p>}
      {editor.notice && <p role="status">{editor.notice}</p>}
      {editor.uncertain && (
        <p className="notice" role="status">
          Wynik zapisu nie jest potwierdzony.{" "}
          <button
            disabled={editor.busy}
            onClick={async () => {
              if (
                await save(
                  path,
                  { ...draft, version: base.version },
                  "PUT",
                  true,
                )
              ) {
                setReason("");
                setTarget("");
              }
            }}
          >
            Potwierdź wynik ostatniej operacji
          </button>
        </p>
      )}
      {editor.conflict && (
        <section className="panel" aria-label="Konflikt wersji">
          <h2>Konflikt wersji</h2>
          <ConflictComparison
            proposal={draft}
            current={fields(editor.conflict)}
          />
          <button
            onClick={() => {
              setBase(editor.conflict!);
              editor.resolve();
            }}
          >
            Zachowaj propozycję i użyj aktualnej wersji
          </button>
          <button
            onClick={() => {
              setBase(editor.conflict!);
              setDraft(fields(editor.conflict!));
              editor.resolve();
            }}
          >
            Użyj aktualnego problemu
          </button>
        </section>
      )}
      <div className="tabs" role="tablist" aria-label="Sekcje problemu">
        {tabs.map(([key, label], index) => (
          <button
            key={key}
            id={`problem-tab-${key}`}
            role="tab"
            aria-selected={tab === key}
            tabIndex={tab === key ? 0 : -1}
            className={`button button--secondary ${tab === key ? "is-active" : ""}`}
            onClick={() => choose(key!)}
            onKeyDown={(e) => {
              const i =
                e.key === "ArrowRight"
                  ? (index + 1) % tabs.length
                  : e.key === "ArrowLeft"
                    ? (index + tabs.length - 1) % tabs.length
                    : e.key === "Home"
                      ? 0
                      : e.key === "End"
                        ? tabs.length - 1
                        : -1;
              if (i >= 0) {
                e.preventDefault();
                choose(tabs[i]![0]!);
                document.getElementById(`problem-tab-${tabs[i]![0]}`)?.focus();
              }
            }}
          >
            {label}
          </button>
        ))}
      </div>
      <section hidden={tab !== "details"}>
        <form
          onSubmit={async (e) => {
            e.preventDefault();
            await save(path, { ...draft, version: base.version }, "PUT", true);
          }}
        >
          <ProblemForm
            draft={draft}
            change={setDraft}
            disabled={blocked || !r.can_edit}
            canAssign={r.can_assign}
            canKnownError={r.can_known_error}
            assignedName={r.assigned_name}
            groupName={r.group_name}
          />
          {r.can_edit && (
            <button
              className="button button--primary"
              disabled={blocked || !dirty}
            >
              Zapisz problem
            </button>
          )}
        </form>
        {!!r.actions.length && (
          <form
            className="form-grid"
            onSubmit={async (e) => {
              e.preventDefault();
              if (
                await save(`${path}/transition`, {
                  version: r.version,
                  state: target,
                  reason,
                })
              ) {
                setReason("");
                setTarget("");
              }
            }}
          >
            <label>
              Przejście procesu
              <select
                aria-label="Przejście procesu"
                disabled={blocked || dirty}
                value={target}
                onChange={(e) => setTarget(e.target.value)}
              >
                <option value="">Wybierz przejście</option>
                {r.actions.map((v) => (
                  <option key={v} value={v}>
                    {labels[v]}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Uzasadnienie
              <textarea
                maxLength={10000}
                disabled={blocked}
                value={reason}
                onChange={(e) => setReason(e.target.value)}
              />
            </label>
            <button className="button" disabled={blocked || dirty || !target}>
              Wykonaj przejście
            </button>
            <p>
              Przejście sprawdza wymagane pola i zadania. Powiązane incydenty
              zachowują własny stan.
            </p>
          </form>
        )}
      </section>
      <section hidden={tab !== "activity"}>
        <CommunicationPanel record={r} apiBase={path} retain={retain} />
      </section>
      {(tab === "related" || relatedPending) && (
        <section className="panel" hidden={tab !== "related"}>
          {dirty ? (
            <PageState title="Zapisz lub odrzuć edycję przed zmianą powiązań" />
          ) : (
            <>
              <ProblemChanges id={r.id} canLink={r.can_edit} />
              <ProblemIncidents
                retain={retain}
                record={r}
                onChanged={() => save(path, undefined, "GET").then(() => {})}
              />
              {(hasPermission("cmdb.read") ||
                hasPermission("cmdb.read_own")) && (
                <RecordCIPanel
                  retain={retain}
                  kind="problems"
                  id={r.id}
                  onChanged={() => save(path, undefined, "GET").then(() => {})}
                />
              )}
            </>
          )}
        </section>
      )}
      <section hidden={tab !== "tasks"}>
        <ProblemTasks
          record={r}
          blocked={blocked || dirty}
          retain={retain}
          onChanged={() => save(path, undefined, "GET").then(() => {})}
        />
      </section>
      {tab === "history" && <InventoryHistory path={`${path}/history`} />}
    </>
  );
}
