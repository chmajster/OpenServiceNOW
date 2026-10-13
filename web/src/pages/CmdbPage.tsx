import { useEffect, useRef, useState } from "react";
import {
  Link,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { PageState } from "../components/PageState";
import { RecordHeader } from "../components/RecordHeader";
import { useIncidentQuery } from "../incidents/hooks";
import { UnsavedGuard } from "../incidents/UnsavedGuard";
import { useCreation } from "../requests/useCreation";
import { useEditor } from "../requests/useEditor";
import { ListToolbar } from "../recordlists/ListToolbar";
import { RecordTable, type RecordColumn } from "../recordlists/RecordTable";
import { columnsFrom, type FilterField } from "../recordlists/contracts";
import {
  useWorkspace,
  useWorkspaceRecord,
} from "../workspace/WorkspaceContext";
import {
  InventoryChoice,
  InventoryHistory,
  LinkedRecords,
  RelationsPanel,
} from "../cmdb/components";
import { labels, type CI, type Metadata } from "../cmdb/contracts";

type Values = Pick<
  CI,
  | "identifier"
  | "name"
  | "class"
  | "status"
  | "owner_id"
  | "environment"
  | "location"
  | "attributes"
  | "support_group_id"
  | "criticality"
  | "lifecycle"
>;
function values(r: CI): Values {
  return {
    identifier: r.identifier,
    name: r.name,
    class: r.class,
    status: r.status,
    owner_id: r.owner_id,
    environment: r.environment,
    location: r.location,
    attributes: r.attributes,
    support_group_id: r.support_group_id,
    criticality: r.criticality,
    lifecycle: r.lifecycle,
  };
}
const options = (keys: string[]) =>
  keys.map((value) => ({ value, label: labels[value] ?? value }));
const states = ["PLANNED", "ACTIVE", "MAINTENANCE", "RETIRED"];
const environments = ["DEV", "TEST", "STAGING", "PROD"];
const criticalities = ["CRITICAL", "HIGH", "NORMAL", "LOW"];
const lifecycles = ["DESIGN", "BUILD", "OPERATE", "RETIRE"];

export function CmdbPage() {
  const { hasPermission } = useAuth(),
    { ciID } = useParams();
  const meta = useIncidentQuery<Metadata>("/api/v1/cmdb/classes");
  if (!hasPermission("cmdb.read") && !hasPermission("cmdb.read_own"))
    return <PageState title="Brak uprawnień do CMDB" />;
  if (meta.loading) return <PageState title="Ładowanie klas CI…" />;
  if (meta.error || !meta.data)
    return (
      <PageState
        title="Nie udało się pobrać klas"
        detail={meta.error ?? undefined}
        tone="error"
        action={<button onClick={meta.reload}>Spróbuj ponownie</button>}
      />
    );
  return ciID ? (
    <Details key={ciID} id={ciID} metadata={meta.data} />
  ) : (
    <List metadata={meta.data} />
  );
}
function List({ metadata }: { metadata: Metadata }) {
  const [params, setParams] = useSearchParams(),
    { hasPermission } = useAuth();
  const query = new URLSearchParams(params);
  for (const key of ["columns", "tab"]) query.delete(key);
  if (!query.has("limit")) query.set("limit", "25");
  const p = useIncidentQuery<{
    items: CI[];
    total: string;
    groups: { value: string; count: string }[];
    next_before_id?: string;
    next_before_value?: string;
  }>(`/api/v1/cmdb/cis?${query}`);
  type Row = CI & { number: string };
  const cols: RecordColumn<Row>[] = [
    {
      key: "number",
      label: "Identyfikator",
      width: 170,
      render: (r) => <Link to={`/cmdb/${r.id}`}>{r.identifier}</Link>,
    },
    { key: "title", label: "Nazwa", width: 260, render: (r) => r.name },
    {
      key: "class",
      label: "Klasa",
      width: 160,
      render: (r) => labels[r.class],
    },
    {
      key: "state",
      label: "Stan operacyjny",
      width: 140,
      render: (r) => labels[r.status],
    },
    {
      key: "environment",
      label: "Środowisko",
      width: 130,
      render: (r) => labels[r.environment],
    },
    {
      key: "owner",
      label: "Właściciel",
      width: 160,
      render: (r) => r.owner_name,
    },
    {
      key: "group",
      label: "Grupa wsparcia",
      width: 170,
      render: (r) => r.support_group_name || "Nieprzypisana",
    },
    {
      key: "criticality",
      label: "Krytyczność",
      width: 120,
      render: (r) => labels[r.criticality],
    },
    {
      key: "lifecycle",
      label: "Cykl życia",
      width: 120,
      render: (r) => labels[r.lifecycle],
    },
    {
      key: "updated_at",
      label: "Aktualizacja",
      width: 170,
      render: (r) => new Date(r.updated_at).toLocaleString("pl-PL"),
    },
  ];
  const fields: FilterField[] = [
    { key: "name", label: "Nazwa", kind: "text" },
    { key: "identifier", label: "Identyfikator", kind: "text" },
    {
      key: "class",
      label: "Klasa",
      kind: "enum",
      options: metadata.classes.map((c) => ({ value: c.key, label: c.label })),
    },
    {
      key: "status",
      label: "Stan operacyjny",
      kind: "enum",
      options: options(states),
    },
    {
      key: "environment",
      label: "Środowisko",
      kind: "enum",
      options: options(environments),
    },
    {
      key: "criticality",
      label: "Krytyczność",
      kind: "enum",
      options: options(criticalities),
    },
    {
      key: "lifecycle",
      label: "Cykl życia",
      kind: "enum",
      options: options(lifecycles),
    },
    {
      key: "owner_id",
      label: "Właściciel",
      kind: "id",
      endpoint: "/api/v1/cmdb/lookups/users",
      collection: "items",
    },
    {
      key: "support_group_id",
      label: "Grupa wsparcia",
      kind: "id",
      endpoint: "/api/v1/cmdb/lookups/groups",
      collection: "items",
      nullable: true,
    },
    { key: "archived", label: "Archiwum", kind: "boolean" },
  ];
  const [search, setSearch] = useState(params.get("q") ?? "");
  useEffect(() => setSearch(params.get("q") ?? ""), [params]);
  function first(next: URLSearchParams) {
    next.delete("before_id");
    next.delete("before_value");
    setParams(next);
  }
  return (
    <>
      <header className="page-heading">
        <div>
          <p className="eyebrow">CMDB</p>
          <h1>Elementy konfiguracji</h1>
        </div>
        {hasPermission("cmdb.manage") && hasPermission("cmdb.read") && (
          <Link className="button button--primary" to="/cmdb/new">
            Nowy CI
          </Link>
        )}
      </header>
      <form
        className="filter-grid"
        onSubmit={(e) => {
          e.preventDefault();
          const next = new URLSearchParams(params);
          search.trim() ? next.set("q", search.trim()) : next.delete("q");
          first(next);
        }}
      >
        <label>
          Wyszukaj CI
          <input
            aria-label="Wyszukaj CI"
            value={search}
            maxLength={160}
            onChange={(e) => setSearch(e.target.value)}
          />
        </label>
        <button className="button">Zastosuj wyszukiwanie</button>
        <button
          type="button"
          className="button button--secondary"
          onClick={p.reload}
        >
          Odśwież listę
        </button>
      </form>
      <ListToolbar
        domain="cmdb"
        fields={fields}
        columns={cols}
        sorts={options(["newest", "oldest", "title", "updated"])}
        groups={fields
          .filter((f) =>
            [
              "class",
              "status",
              "environment",
              "criticality",
              "lifecycle",
              "support_group_id",
            ].includes(f.key),
          )
          .map((f) => ({ value: f.key, label: f.label }))}
        apiRoot="/api/v1/cmdb/cis"
        total={p.data?.total}
        counts={p.data?.groups}
      />
      {p.loading ? (
        <PageState title="Ładowanie CI…" />
      ) : p.error ? (
        <PageState
          title="Nie udało się pobrać CI"
          detail={p.error}
          tone="error"
          action={<button onClick={p.reload}>Spróbuj ponownie</button>}
        />
      ) : !p.data?.items.length ? (
        <PageState title="Brak CI dla wybranych filtrów" />
      ) : (
        <RecordTable
          records={p.data.items.map((r) => ({ ...r, number: r.identifier }))}
          columns={cols}
          selected={columnsFrom(params.get("columns"), cols)}
          url={(r) => `/cmdb/${r.id}`}
          apiRoot="/api/v1/cmdb/cis"
          stateLabel={(s) => labels[s] ?? s}
        />
      )}
      <div className="action-row">
        {params.has("before_id") && (
          <button
            className="button"
            onClick={() => first(new URLSearchParams(params))}
          >
            Pierwsza strona
          </button>
        )}
        {p.data?.next_before_id && (
          <button
            className="button"
            onClick={() => {
              const next = new URLSearchParams(params);
              next.set("before_id", p.data!.next_before_id!);
              if (p.data!.next_before_value)
                next.set("before_value", p.data!.next_before_value);
              else next.delete("before_value");
              setParams(next);
            }}
          >
            Następna strona
          </button>
        )}
      </div>
    </>
  );
}
function Details({ id, metadata }: { id: string; metadata: Metadata }) {
  const p = useIncidentQuery<CI>(
    id === "new" ? null : `/api/v1/cmdb/cis/${id}`,
  );
  if (id === "new") return <Create metadata={metadata} />;
  if (p.loading) return <PageState title="Ładowanie CI…" />;
  if (p.error || !p.data)
    return (
      <PageState
        title="Nie udało się otworzyć CI"
        detail={p.error ?? undefined}
        tone="error"
        action={<button onClick={p.reload}>Spróbuj ponownie</button>}
      />
    );
  return <Editor initial={p.data} metadata={metadata} />;
}
function Create({ metadata }: { metadata: Metadata }) {
  const { session, hasPermission } = useAuth(),
    navigate = useNavigate(),
    create = useCreation<CI>();
  const [draft, setDraft] = useState<Values>({
    identifier: "",
    name: "",
    class: "PHYSICAL_SERVER",
    status: "PLANNED",
    owner_id: session!.user.id,
    environment: "PROD",
    location: "",
    attributes: {},
    criticality: "NORMAL",
    lifecycle: "DESIGN",
  });
  const original = useRef(JSON.stringify(draft));
  const [saved, setSaved] = useState("");
  useEffect(() => {
    if (saved) navigate(saved, { replace: true });
  }, [saved, navigate]);
  if (!hasPermission("cmdb.manage") || !hasPermission("cmdb.read"))
    return <PageState title="Brak uprawnień do utworzenia CI" />;
  return (
    <>
      <UnsavedGuard
        dirty={
          !saved &&
          (JSON.stringify(draft) !== original.current || create.uncertain)
        }
      />
      <RecordHeader
        number="Nowy CI"
        title="Nowy element konfiguracji"
        type="CMDB"
        state={labels[draft.status]!}
        version="—"
        actions={
          <Link className="button" to="/cmdb">
            Wróć do listy
          </Link>
        }
      />
      <form
        onSubmit={async (e) => {
          e.preventDefault();
          const result = await create.create("/api/v1/cmdb/cis", draft);
          if (result) setSaved(`/cmdb/${result.id}`);
        }}
      >
        <CIForm
          draft={draft}
          change={setDraft}
          metadata={metadata}
          disabled={create.busy || create.uncertain}
          ownerName={session!.user.display_name}
        />
        {create.error && <p role="alert">{create.error}</p>}
        {create.uncertain && (
          <p role="status">
            Wynik zapisu jest nieznany. Ponowienie użyje tego samego klucza i
            danych; nie utworzy drugiego CI.
          </p>
        )}
        <button
          className="button button--primary"
          disabled={
            create.busy ||
            (!create.uncertain &&
              (!draft.name.trim() ||
                !draft.identifier.trim() ||
                !draft.owner_id))
          }
        >
          {create.uncertain ? "Potwierdź wynik zapisu" : "Utwórz CI"}
        </button>
      </form>
    </>
  );
}
function Editor({ initial, metadata }: { initial: CI; metadata: Metadata }) {
  const path = `/api/v1/cmdb/cis/${initial.id}`,
    editor = useEditor(initial, path),
    r = editor.record;
  const [base, setBase] = useState(initial),
    [draft, setDraft] = useState(values(initial)),
    [reason, setReason] = useState("");
  const operations = useRef<Record<string, string>>({});
  const accept = useRef(false),
    dirty = JSON.stringify(draft) !== JSON.stringify(values(base));
  const { hasPermission } = useAuth(),
    workspace = useWorkspace(),
    [params, setParams] = useSearchParams(),
    tab = params.get("tab") ?? "details";
  useEffect(() => {
    if (r.version !== base.version && (accept.current || !dirty)) {
      accept.current = false;
      setBase(r);
      setDraft(values(r));
    }
  }, [r, base.version, dirty]);
  useWorkspaceRecord(
    {
      number: r.identifier,
      title: r.name,
      state: labels[r.status] ?? r.status,
      type: "CI",
      assignment: [r.owner_name, r.support_group_name]
        .filter(Boolean)
        .join(" · "),
    },
    r.id,
  );
  const manage = hasPermission("cmdb.manage") && hasPermission("cmdb.read");
  async function save(path: string, body: unknown, method = "POST") {
    accept.current = !dirty;
    const fingerprint = method + path + JSON.stringify(body);
    const key = operations.current[fingerprint] ?? crypto.randomUUID();
    operations.current = { [fingerprint]: key };
    const result = await editor.save(path, body, method, {
      "Idempotency-Key": key,
    });
    if (!result) accept.current = false;
    return result;
  }
  function resolve(keep: boolean) {
    if (!editor.conflict) return;
    const current = editor.conflict;
    setBase(current);
    if (!keep) setDraft(values(current));
    editor.resolve();
  }
  return (
    <>
      <UnsavedGuard dirty={dirty || !!reason} />
      <RecordHeader
        number={r.identifier}
        title={r.name}
        type={labels[r.class] ?? r.class}
        state={`${labels[r.status]}${r.archived ? " · Archiwum" : ""}`}
        version={r.version}
        assignment={[r.owner_name, r.support_group_name]
          .filter(Boolean)
          .join(" · ")}
        severity={labels[r.criticality]}
        actions={
          <>
            <Link
              className="button button--secondary"
              to={workspace?.listURL("/cmdb") ?? "/cmdb"}
            >
              Wróć do listy
            </Link>
            <button
              className="button"
              disabled={editor.busy || !!editor.conflict}
              onClick={() => void save(path, undefined, "GET")}
            >
              Odśwież CI
            </button>
          </>
        }
      />
      {editor.error && <p role="alert">{editor.error}</p>}
      {editor.notice && <p role="status">{editor.notice}</p>}
      {editor.conflict && (
        <section className="panel" aria-label="Konflikt wersji">
          <h2>Konflikt wersji</h2>
          <div className="conflict-columns">
            <div>
              <h3>Twoja propozycja</h3>
              <pre>{JSON.stringify(draft, null, 2)}</pre>
            </div>
            <div>
              <h3>Aktualny CI</h3>
              <pre>{JSON.stringify(values(editor.conflict), null, 2)}</pre>
            </div>
          </div>
          <button className="button" onClick={() => resolve(true)}>
            Zachowaj propozycję i użyj aktualnej wersji
          </button>
          <button
            className="button button--secondary"
            onClick={() => resolve(false)}
          >
            Użyj aktualnego CI
          </button>
        </section>
      )}
      <div className="tabs" role="tablist" aria-label="Sekcje CI">
        {[
          ["details", "Szczegóły"],
          ["relations", "Zależności"],
          ["records", "Powiązane rekordy"],
          ["history", "Historia"],
        ].map(([key, label]) => (
          <button
            key={key}
            role="tab"
            id={`ci-tab-${key}`}
            tabIndex={tab === key ? 0 : -1}
            className={`button button--secondary ${tab === key ? "is-active" : ""}`}
            onKeyDown={(e) => {
              const keys = ["details", "relations", "records", "history"];
              const index = keys.indexOf(key!);
              const target =
                e.key === "ArrowRight"
                  ? keys[(index + 1) % keys.length]
                  : e.key === "ArrowLeft"
                    ? keys[(index + keys.length - 1) % keys.length]
                    : e.key === "Home"
                      ? keys[0]
                      : e.key === "End"
                        ? keys.at(-1)
                        : undefined;
              if (target) {
                e.preventDefault();
                const next = new URLSearchParams(params);
                next.set("tab", target);
                setParams(next);
                document.getElementById(`ci-tab-${target}`)?.focus();
              }
            }}
            aria-selected={tab === key}
            onClick={() => {
              const next = new URLSearchParams(params);
              next.set("tab", key!);
              setParams(next);
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
            accept.current = true;
            const result = await editor.save(
              path,
              { ...draft, version: base.version },
              "PUT",
            );
            if (!result) accept.current = false;
          }}
        >
          <CIForm
            draft={draft}
            change={setDraft}
            metadata={metadata}
            disabled={editor.busy || !!editor.conflict || !manage || r.archived}
            immutableClass
            ownerName={r.owner_name}
            groupName={r.support_group_name}
          />
          {manage && !r.archived && (
            <button
              className="button button--primary"
              disabled={!dirty || editor.busy || !!editor.conflict}
            >
              Zapisz CI
            </button>
          )}
        </form>
        {manage && (
          <form
            className="form-grid"
            onSubmit={async (e) => {
              e.preventDefault();
              if (
                await save(`${path}/archive`, {
                  version: r.version,
                  archived: !r.archived,
                  reason,
                })
              )
                setReason("");
            }}
          >
            <label>
              Uzasadnienie archiwizacji lub przywrócenia
              <textarea
                maxLength={1000}
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                disabled={editor.busy}
              />
            </label>
            <button
              className="button button--secondary"
              disabled={
                dirty || !reason.trim() || editor.busy || !!editor.conflict
              }
            >
              {r.archived ? "Przywróć CI" : "Archiwizuj CI"}
            </button>
            <p>Archiwizacja zachowuje historię i dotychczasowe powiązania.</p>
          </form>
        )}
      </section>
      {tab === "relations" && (
        <RelationsPanel
          record={r}
          busy={editor.busy || dirty || !!editor.conflict}
          save={save}
        />
      )}
      {tab === "records" && <LinkedRecords id={r.id} />}
      {tab === "history" && <InventoryHistory path={`${path}/history`} />}
    </>
  );
}
function CIForm({
  draft,
  change,
  metadata,
  disabled,
  immutableClass = false,
  ownerName = "",
  groupName = "",
}: {
  draft: Values;
  change: (v: Values) => void;
  metadata: Metadata;
  disabled: boolean;
  immutableClass?: boolean;
  ownerName?: string;
  groupName?: string;
}) {
  const clazz = metadata.classes.find((c) => c.key === draft.class)!;
  const [referenceLabels, setReferenceLabels] = useState<
    Record<string, string>
  >({
    ["owner:" + draft.owner_id]: ownerName,
    ...(draft.support_group_id
      ? { ["group:" + draft.support_group_id]: groupName }
      : {}),
  });
  function set(key: keyof Values, value: unknown) {
    change({ ...draft, [key]: value });
  }
  return (
    <fieldset className="form-fields" disabled={disabled}>
      <div className="form-grid">
        <label>
          Identyfikator CI
          <input
            aria-label="Identyfikator CI"
            required
            maxLength={100}
            pattern="[a-zA-Z0-9][a-zA-Z0-9._:-]*"
            value={draft.identifier}
            onChange={(e) => set("identifier", e.target.value)}
          />
        </label>
        <label>
          Nazwa CI
          <input
            aria-label="Nazwa CI"
            required
            maxLength={160}
            value={draft.name}
            onChange={(e) => set("name", e.target.value)}
          />
        </label>
        <label>
          Klasa CI
          <select
            aria-label="Klasa CI"
            value={draft.class}
            disabled={disabled || immutableClass}
            onChange={(e) =>
              change({ ...draft, class: e.target.value, attributes: {} })
            }
          >
            {metadata.classes.map((c) => (
              <option key={c.key} value={c.key}>
                {c.label}
              </option>
            ))}
          </select>
        </label>
        {(
          [
            ["status", "Stan operacyjny", states],
            ["environment", "Środowisko", environments],
            ["criticality", "Krytyczność", criticalities],
            ["lifecycle", "Etap cyklu życia", lifecycles],
          ] as const
        ).map(([key, label, keys]) => (
          <label key={key}>
            {label}
            <select
              aria-label={label}
              value={draft[key]}
              onChange={(e) => set(key, e.target.value)}
            >
              {keys.map((k) => (
                <option key={k} value={k}>
                  {labels[k]}
                </option>
              ))}
            </select>
          </label>
        ))}
        <InventoryChoice
          label="Właściciel CI"
          value={
            draft.owner_id
              ? {
                  id: draft.owner_id,
                  label:
                    referenceLabels["owner:" + draft.owner_id] ||
                    ownerName ||
                    draft.owner_id,
                }
              : null
          }
          onChange={(v) => {
            if (v)
              setReferenceLabels((p) => ({ ...p, ["owner:" + v.id]: v.label }));
            set("owner_id", v?.id ?? "");
          }}
          endpoint="/api/v1/cmdb/lookups/users"
          disabled={disabled}
        />
        <InventoryChoice
          label="Grupa wsparcia"
          value={
            draft.support_group_id
              ? {
                  id: draft.support_group_id,
                  label:
                    referenceLabels["group:" + draft.support_group_id] ||
                    groupName ||
                    draft.support_group_id,
                }
              : null
          }
          onChange={(v) => {
            if (v)
              setReferenceLabels((p) => ({ ...p, ["group:" + v.id]: v.label }));
            set("support_group_id", v?.id);
          }}
          endpoint="/api/v1/cmdb/lookups/groups"
          disabled={disabled}
        />
        <label>
          Lokalizacja
          <input
            value={draft.location}
            maxLength={160}
            onChange={(e) => set("location", e.target.value)}
          />
        </label>
      </div>
      <h2>Pola klasy: {clazz.label}</h2>
      <div className="form-grid">
        {clazz.attributes.map((a) => (
          <label key={a.key}>
            {a.label}
            {a.type === "SELECT" ? (
              <select
                aria-label={a.label}
                value={draft.attributes[a.key] ?? ""}
                onChange={(e) => {
                  const attributes = { ...draft.attributes };
                  if (e.target.value) attributes[a.key] = e.target.value;
                  else delete attributes[a.key];
                  set("attributes", attributes);
                }}
              >
                <option value="">Nie określono</option>
                {a.options?.map((k) => (
                  <option key={k} value={k}>
                    {labels[k] ?? k}
                  </option>
                ))}
              </select>
            ) : (
              <input
                aria-label={a.label}
                type={
                  a.type === "INTEGER" || a.type === "NUMBER"
                    ? "number"
                    : a.type === "URL"
                      ? "url"
                      : "text"
                }
                min={a.min}
                max={a.max}
                step={a.type === "INTEGER" ? 1 : "any"}
                maxLength={a.max_length ?? 2048}
                value={draft.attributes[a.key] ?? ""}
                onChange={(e) => {
                  const attributes = { ...draft.attributes };
                  if (e.target.value)
                    attributes[a.key] =
                      a.type === "INTEGER" || a.type === "NUMBER"
                        ? Number(e.target.value)
                        : e.target.value;
                  else delete attributes[a.key];
                  set("attributes", attributes);
                }}
              />
            )}
          </label>
        ))}
      </div>
    </fieldset>
  );
}
