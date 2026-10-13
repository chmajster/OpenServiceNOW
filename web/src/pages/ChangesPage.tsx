import { useCallback, useEffect, useRef, useState } from "react";
import {
  Link,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { ConflictComparison } from "../components/ConflictComparison";
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
import { ChangeApprovals, ChangeCollisions } from "../changes/process";
import { ChangeForm } from "../changes/ChangeForm";
import { ChangeProblems, ChangeTasks } from "../changes/panels";
import {
  blank,
  fields,
  labels,
  states,
  type Change,
  type ChangeFields,
  type SourceProblem,
} from "../changes/contracts";

const options = (keys: string[]) =>
  keys.map((value) => ({ value, label: labels[value] ?? value }));
export function ChangesPage() {
  const { changeID } = useParams(),
    { hasPermission } = useAuth();
  const p = useIncidentQuery<Change>(
    changeID && changeID !== "new" ? `/api/v1/changes/${changeID}` : null,
  );
  if (!hasPermission("change.read") && !hasPermission("change.read_own"))
    return <PageState title="Brak uprawnień do zmian" />;
  if (!changeID) return <ChangeList />;
  if (changeID === "new") return <CreateChange />;
  if (p.loading) return <PageState title="Ładowanie zmiany…" />;
  if (p.error || !p.data)
    return (
      <PageState
        title="Nie udało się otworzyć zmiany"
        tone="error"
        detail={p.error ?? undefined}
        action={<button onClick={p.reload}>Spróbuj ponownie</button>}
      />
    );
  return <Editor key={changeID} initial={p.data} />;
}
export function CabPage() {
  const { hasPermission } = useAuth(),
    [params, setParams] = useSearchParams();
  useEffect(() => {
    if (params.get("state") !== "AUTHORIZE") {
      const n = new URLSearchParams(params);
      n.set("state", "AUTHORIZE");
      setParams(n, { replace: true });
    }
  }, [params, setParams]);
  if (!["change.read", "change.read_own"].some(hasPermission))
    return <PageState title="Brak uprawnień do CAB" />;
  return params.get("state") === "AUTHORIZE" ? (
    <ChangeList cab />
  ) : (
    <PageState title="Ładowanie CAB…" />
  );
}
function CabReview({ record }: { record: Change }) {
  const [open, setOpen] = useState(false);
  return (
    <details onToggle={(e) => setOpen(e.currentTarget.open)}>
      <summary>Decyzje, kolizje i historia</summary>
      {open && (
        <>
          <ChangeApprovals record={record} />
          <ChangeCollisions record={record} />
          <InventoryHistory path={`/api/v1/changes/${record.id}/history`} />
        </>
      )}
    </details>
  );
}
export function ChangeList({ cab = false }: { cab?: boolean } = {}) {
  const [params, setParams] = useSearchParams(),
    { hasPermission } = useAuth();
  const q = new URLSearchParams(params);
  q.delete("columns");
  if (cab) q.set("state", "AUTHORIZE");
  if (!q.has("limit")) q.set("limit", "25");
  const p = useIncidentQuery<{
    items: Change[];
    total: string;
    groups: { value: string; count: string }[];
    next_before_id?: string;
    next_before_value?: string;
  }>(`/api/v1/changes/?${q}`);
  const [search, setSearch] = useState(params.get("q") ?? "");
  useEffect(() => setSearch(params.get("q") ?? ""), [params]);
  const cols: RecordColumn<Change>[] = [
    {
      key: "number",
      label: "Numer",
      width: 160,
      render: (r) => <Link to={`/changes/${r.id}`}>{r.number}</Link>,
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
      label: "Wykonawca",
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
      key: "risk",
      label: "Ryzyko",
      width: 100,
      render: (r) => labels[r.risk],
    },
    {
      key: "change_type",
      label: "Typ",
      width: 100,
      render: (r) => labels[r.change_type],
    },
    {
      key: "planned_start",
      label: "Planowany początek",
      width: 190,
      render: (r) =>
        r.planned_start
          ? new Date(r.planned_start).toLocaleString("pl-PL", {
              timeZone: r.time_zone,
            })
          : "Nie zaplanowano",
    },
    {
      key: "approval",
      label: "Akceptacja",
      width: 190,
      render: (r) => (
        <>
          {labels[r.approval_state]}
          {r.execution_id && (
            <>
              {" "}
              ·{" "}
              <Link to={`/approvals/${r.execution_id}`}>
                Decyzje · wersja {r.evaluated_version}
              </Link>
            </>
          )}
        </>
      ),
    },
    {
      key: "updated_at",
      label: "Aktualizacja",
      width: 170,
      render: (r) => new Date(r.updated_at).toLocaleString("pl-PL"),
    },
  ];
  if (cab)
    cols.push({
      key: "cab_review",
      label: "Ocena CAB",
      width: 280,
      render: (r) => <CabReview record={r} />,
    });
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
    {
      key: "risk",
      label: "Ryzyko",
      kind: "enum",
      options: options(["HIGH", "MEDIUM", "LOW"]),
    },
    {
      key: "change_type",
      label: "Typ zmiany",
      kind: "enum",
      options: options(["NORMAL", "STANDARD", "EMERGENCY"]),
    },
    {
      key: "assignment_group_id",
      label: "Grupa",
      kind: "id",
      nullable: true,
      endpoint: "/api/v1/changes/lookups/groups",
      collection: "items",
    },
    {
      key: "assigned_to",
      label: "Wykonawca",
      kind: "id",
      nullable: true,
      endpoint: "/api/v1/changes/lookups/users",
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
          <p className="eyebrow">Change Management</p>
          <h1>{cab ? "CAB — ocena zmian" : "Zmiany"}</h1>
        </div>
        {hasPermission("change.create") && (
          <Link className="button button--primary" to="/changes/new">
            Nowa zmiana
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
          Wyszukaj zmianę
          <input
            value={search}
            maxLength={160}
            onChange={(e) => setSearch(e.target.value)}
          />
        </label>
        <button className="button">Zastosuj wyszukiwanie</button>
        <button type="button" className="button" onClick={p.reload}>
          Odśwież listę
        </button>
      </form>
      <ListToolbar
        domain="changes"
        fields={filters}
        columns={cols}
        sorts={options(["newest", "oldest", "title", "updated"])}
        groups={filters
          .filter((f) =>
            [
              "state",
              "impact",
              "assignment_group_id",
              "risk",
              "change_type",
            ].includes(f.key),
          )
          .map((f) => ({ value: f.key, label: f.label }))}
        apiRoot="/api/v1/changes"
        total={p.data?.total}
        counts={p.data?.groups}
      />
      {p.loading ? (
        <PageState title="Ładowanie zmian…" />
      ) : p.error ? (
        <PageState
          title="Błąd listy zmian"
          tone="error"
          detail={p.error}
          action={<button onClick={p.reload}>Spróbuj ponownie</button>}
        />
      ) : !p.data?.items.length ? (
        <PageState title="Brak zmian dla wybranych filtrów" />
      ) : (
        <RecordTable
          records={p.data.items}
          columns={cols}
          selected={columnsFrom(
            params.get("columns") ??
              (cab
                ? "number,title,risk,planned_start,group,approval,cab_review"
                : null),
            cols,
          )}
          apiRoot="/api/v1/changes"
          url={(r) => `/changes/${r.id}`}
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
function CreateChange() {
  const { hasPermission } = useAuth(),
    [params] = useSearchParams(),
    navigate = useNavigate(),
    creation = useCreation<Change>();
  const sourceID = params.get("source_problem_id"),
    source = useIncidentQuery<SourceProblem>(
      sourceID ? `/api/v1/changes/source-problems/${sourceID}` : null,
    );
  const [draft, setDraft] = useState<ChangeFields>({ ...blank }),
    [copied, setCopied] = useState(false),
    [saved, setSaved] = useState("");
  useEffect(() => {
    if (saved) navigate(saved, { replace: true });
  }, [saved, navigate]);
  if (!hasPermission("change.create"))
    return <PageState title="Brak uprawnień do rejestracji zmiany" />;
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
        number="Nowy CHG"
        title="Rejestracja zmiany"
        type="Zmiana"
        state="Rejestracja"
        version="—"
        actions={
          <Link className="button" to="/changes">
            Wróć do listy
          </Link>
        }
      />
      {sourceID && (
        <section className="panel">
          <h2>Podgląd informacji z problemu</h2>
          {source.loading ? (
            <PageState title="Ładowanie podglądu…" />
          ) : source.error ? (
            <PageState
              title="Brak dostępnego problemu"
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
                <p className="preserve-lines">
                  Plan trwałego rozwiązania: {source.data.solution_plan}
                </p>
                <p>
                  Kopiowane pola: opis skrócony, opis, wpływ i plan trwałego
                  rozwiązania. Notatki i pliki nie są przenoszone.
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
                      implementation_plan: source.data!.solution_plan,
                    }));
                    setCopied(true);
                  }}
                >
                  Użyj pokazanych informacji i powiąż problem
                </button>
              </>
            )
          )}
        </section>
      )}
      <form
        onSubmit={async (e) => {
          e.preventDefault();
          const r = await creation.create("/api/v1/changes/", {
            ...draft,
            ...(source.data && copied
              ? {
                  source_problem_id: source.data.id,
                  source_version: source.data.version,
                }
              : {}),
          });
          if (r) setSaved(`/changes/${r.id}`);
        }}
      >
        <ChangeForm
          draft={draft}
          change={setDraft}
          disabled={creation.busy || creation.uncertain}
          canAssign={hasPermission("change.assign")}
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
          {creation.uncertain ? "Potwierdź wynik zapisu" : "Utwórz zmianę"}
        </button>
      </form>
    </>
  );
}
function Editor({ initial }: { initial: Change }) {
  const path = `/api/v1/changes/${initial.id}`,
    editor = useEditor(initial, path),
    r = editor.record,
    workspace = useWorkspace(),
    { hasPermission } = useAuth();
  const [base, setBase] = useState(initial),
    [draft, setDraft] = useState(fields(initial)),
    [reason, setReason] = useState(""),
    [target, setTarget] = useState(""),
    [override, setOverride] = useState(false);
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
      type: "Zmiana",
      assignment: [r.group_name, r.assigned_name].filter(Boolean).join(" · "),
      links: [
        { url: `/changes/${r.id}?tab=related`, label: "Problemy i CI" },
        { url: `/changes/${r.id}?tab=tasks`, label: "Zadania wykonawcze" },
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
    ["approvals", "Akceptacje"],
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
  const blocked = editor.busy || !!editor.conflict || editor.uncertain;
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
        type="Zmiana"
        state={labels[r.state] ?? r.state}
        version={r.version}
        severity={
          <span>
            Wpływ: {labels[r.impact]}· {labels[r.change_type]} · Ryzyko:{" "}
            {labels[r.risk]}
          </span>
        }
        assignment={[r.group_name, r.assigned_name].filter(Boolean).join(" · ")}
        actions={
          <>
            <Link
              className="button"
              to={workspace?.listURL("/changes") ?? "/changes"}
            >
              Wróć do listy
            </Link>
            <button
              disabled={blocked}
              onClick={() => void save(path, undefined, "GET")}
            >
              Odśwież zmianę
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
                setOverride(false);
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
            Użyj aktualnego zapisu zmiany
          </button>
        </section>
      )}
      <div className="tabs" role="tablist" aria-label="Sekcje zmiany">
        {tabs.map(([key, label], index) => (
          <button
            key={key}
            id={`change-tab-${key}`}
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
                document.getElementById(`change-tab-${tabs[i]![0]}`)?.focus();
              }
            }}
          >
            {label}
          </button>
        ))}
      </div>
      {tab === "details" && (
        <dl className="form-grid">
          <div>
            <dt>Rzeczywisty początek</dt>
            <dd>
              {r.actual_start
                ? new Date(r.actual_start).toLocaleString("pl-PL", {
                    timeZone: r.time_zone,
                  })
                : "Nie rozpoczęto"}
            </dd>
          </div>
          <div>
            <dt>Rzeczywisty koniec</dt>
            <dd>
              {r.actual_end
                ? new Date(r.actual_end).toLocaleString("pl-PL", {
                    timeZone: r.time_zone,
                  })
                : "Nie zakończono"}
            </dd>
          </div>
          <div>
            <dt>Akceptacja</dt>
            <dd>
              {labels[r.approval_state]} · Oceniana wersja{" "}
              {r.evaluated_version ?? "brak"} · Zakres {r.assessment_revision} ·{" "}
              {r.time_zone}
            </dd>
          </div>
        </dl>
      )}
      <section hidden={tab !== "details"}>
        <form
          onSubmit={async (e) => {
            e.preventDefault();
            await save(path, { ...draft, version: base.version }, "PUT", true);
          }}
        >
          <ChangeForm
            draft={draft}
            change={setDraft}
            disabled={blocked || !r.can_edit}
            canAssign={r.can_assign}
            existing
            plansLocked={["IMPLEMENT", "REVIEW"].includes(r.state)}
            assignedName={r.assigned_name}
            groupName={r.group_name}
          />
          {r.can_edit && (
            <button
              className="button button--primary"
              disabled={blocked || !dirty}
            >
              Zapisz zmianę
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
                  override: target === "SCHEDULED" && override,
                })
              ) {
                setReason("");
                setTarget("");
                setOverride(false);
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
                required={
                  ["NEW", "ASSESS", "CANCELLED"].includes(target) || override
                }
                disabled={blocked}
                value={reason}
                onChange={(e) => setReason(e.target.value)}
              />
            </label>
            {target === "SCHEDULED" &&
              hasPermission("change.schedule_override") && (
                <label className="checkbox-label">
                  <input
                    type="checkbox"
                    checked={override}
                    disabled={blocked}
                    onChange={(e) => setOverride(e.target.checked)}
                  />
                  Świadomie obejdź kolizję lub okres zakazu; wymagane
                  uzasadnienie zostanie zapisane w audycie.
                </label>
              )}
            <button className="button" disabled={blocked || dirty || !target}>
              Wykonaj przejście
            </button>
            <p>
              Przejście sprawdza wymagane pola i zadania. Wymagane zadania,
              ocena i akceptacja są sprawdzane przez backend. Powiązane problemy
              i incydenty zachowują własny stan.
            </p>
          </form>
        )}
      </section>
      {tab === "approvals" && <ChangeApprovals record={r} />}
      <section hidden={tab !== "activity"}>
        <CommunicationPanel record={r} apiBase={path} retain={retain} />
      </section>
      {tab === "related" && (
        <section className="panel">
          {dirty ? (
            <PageState title="Zapisz lub odrzuć edycję przed zmianą powiązań" />
          ) : (
            <>
              <ChangeProblems
                record={r}
                onChanged={() => save(path, undefined, "GET").then(() => {})}
              />
              {(hasPermission("cmdb.read") ||
                hasPermission("cmdb.read_own")) && (
                <RecordCIPanel
                  kind="changes"
                  id={r.id}
                  onChanged={() => save(path, undefined, "GET").then(() => {})}
                />
              )}
            </>
          )}
        </section>
      )}
      {tab === "details" && r.planned_start && r.planned_end && (
        <ChangeCollisions key={r.version} record={r} />
      )}
      <section hidden={tab !== "tasks"}>
        <ChangeTasks
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
