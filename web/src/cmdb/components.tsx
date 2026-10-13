import { useRetainedCommand } from "../components/useRetainedCommand";
import { useEffect, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { api, ApiError, jsonBody } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import {
  auditChanges,
  displayValue,
  fieldLabel,
} from "../components/auditPresentation";
import { RemoteCombobox, type Choice } from "../components/RemoteCombobox";
import { useRemotePage } from "../components/useRemotePage";
import { PageState } from "../components/PageState";
import { useIncidentQuery } from "../incidents/hooks";
import {
  labels,
  text,
  type CI,
  type Graph,
  type Reference,
  type Relation,
} from "./contracts";

export function InventoryChoice({
  label,
  value,
  onChange,
  endpoint,
  disabled = false,
}: {
  label: string;
  value: Choice | null;
  onChange: (value: Choice | null) => void;
  endpoint: string;
  disabled?: boolean;
}) {
  return (
    <RemoteCombobox
      kind="groups"
      label={label}
      value={value}
      onChange={onChange}
      endpoint={endpoint}
      collection="items"
      cursor="before_id"
      disabled={disabled}
    />
  );
}
export function PageFeedback({
  loading,
  error,
  empty,
}: {
  loading: boolean;
  error?: string | null;
  empty: boolean;
}) {
  return (
    <>
      {loading && <PageState title="Ładowanie…" />}
      {error && (
        <PageState
          title="Nie udało się pobrać danych"
          detail={error}
          tone="error"
        />
      )}
      {!loading && !error && empty && <PageState title="Brak wyników" />}
    </>
  );
}
export function More({
  loading,
  next,
  load,
}: {
  loading: boolean;
  next?: string | number;
  load: () => void;
}) {
  return next ? (
    <button type="button" disabled={loading} onClick={load}>
      Pokaż więcej
    </button>
  ) : null;
}
export function InventoryHistory({ path }: { path: string }) {
  type Entry = {
    id: string;
    actor_name: string;
    summary: string;
    event_time: string;
    type?: string;
    data: {
      before?: unknown;
      after?: unknown;
      body?: string;
      visibility?: string;
      state?: string;
    };
  };
  const p = useRemotePage<Entry>(path + "?limit=25", "entries", "before_id");
  return (
    <>
      <p>
        Historia jest projekcją trwałych zdarzeń. Po zapisie worker może
        potrzebować chwili na jej uzupełnienie.
      </p>
      <button onClick={p.reload} disabled={p.loading}>
        Odśwież historię
      </button>
      <PageFeedback
        loading={p.loading}
        error={p.error}
        empty={!p.items.length}
      />
      <ol className="timeline">
        {p.items.map((e) => (
          <li key={e.id}>
            <strong>{e.summary}</strong>
            <p>
              {e.actor_name} · {new Date(e.event_time).toLocaleString("pl-PL")}
            </p>
            <p className="muted">
              Źródło:{" "}
              {e.type?.startsWith("problem.")
                ? "Proces Problem"
                : e.type?.startsWith("change.")
                  ? "Proces Change"
                  : e.type?.startsWith("ci.")
                    ? "CMDB"
                    : "Rekord"}
              {e.data?.visibility === "INTERNAL" ? " · Wewnętrzne" : ""}
            </p>
            {e.data?.body && <p className="preserve-lines">{e.data.body}</p>}
            {Boolean(e.data?.before || e.data?.after) && (
              <dl>
                {auditChanges(e.data.before, e.data.after).map((c) => (
                  <div key={c.field}>
                    <dt>{fieldLabel(c.field)}</dt>
                    <dd>
                      {displayValue(c.before)} → {displayValue(c.after)}
                    </dd>
                  </div>
                ))}
              </dl>
            )}
            <details>
              <summary>Dane zdarzenia</summary>
              <pre>{JSON.stringify(e.data, null, 2)}</pre>
            </details>
          </li>
        ))}
      </ol>
      <More loading={p.loading} next={p.next} load={p.more} />
    </>
  );
}

export function DependencyGraph({ id }: { id: string }) {
  const [params, setParams] = useSearchParams();
  const get = (k: string, d: string) => params.get(k) ?? d;
  const change = (k: string, v: string) => {
    const n = new URLSearchParams(params);
    v ? n.set(k, v) : n.delete(k);
    setParams(n, { replace: true });
  };
  const query = new URLSearchParams({
    depth: get("depth", "2"),
    max_nodes: get("max_nodes", "50"),
    type: get("relation_type", ""),
    class: get("graph_class", ""),
    environment: get("graph_environment", ""),
  });
  const p = useIncidentQuery<Graph>(`/api/v1/cmdb/cis/${id}/graph?${query}`);
  return (
    <section>
      <h3>Graf zapisanych zależności</h3>
      <p>
        Ręczna ewidencja zależności. Graf nie wykrywa infrastruktury
        automatycznie. Maksymalnie 100 węzłów, 200 relacji i 5 poziomów; cykle
        są zachowane bez powielania węzłów.
      </p>
      <div className="filter-grid">
        <label>
          Głębokość
          <input
            type="number"
            min="0"
            max="5"
            value={get("depth", "2")}
            onChange={(e) => change("depth", e.target.value)}
          />
        </label>
        <label>
          Limit węzłów
          <input
            type="number"
            min="1"
            max="100"
            value={get("max_nodes", "50")}
            onChange={(e) => change("max_nodes", e.target.value)}
          />
        </label>
        <label>
          Typ relacji
          <select
            value={get("relation_type", "")}
            onChange={(e) => change("relation_type", e.target.value)}
          >
            <option value="">Wszystkie</option>
            {[
              "RUNS_ON",
              "HOSTED_ON",
              "DEPENDS_ON",
              "CONNECTS_TO",
              "PART_OF",
            ].map((k) => (
              <option key={k} value={k}>
                {labels[k]}
              </option>
            ))}
          </select>
        </label>
        <label>
          Klasa w grafie
          <select
            value={get("graph_class", "")}
            onChange={(e) => change("graph_class", e.target.value)}
          >
            <option value="">Wszystkie</option>
            {[
              "PHYSICAL_SERVER",
              "VM",
              "NETWORK_DEVICE",
              "APPLICATION",
              "DATABASE",
              "SERVICE",
            ].map((k) => (
              <option key={k} value={k}>
                {labels[k]}
              </option>
            ))}
          </select>
        </label>
        <label>
          Środowisko w grafie
          <select
            value={get("graph_environment", "")}
            onChange={(e) => change("graph_environment", e.target.value)}
          >
            <option value="">Wszystkie</option>
            {["DEV", "TEST", "STAGING", "PROD"].map((k) => (
              <option key={k} value={k}>
                {labels[k]}
              </option>
            ))}
          </select>
        </label>
      </div>
      <PageFeedback loading={p.loading} error={p.error} empty={false} />
      {p.data && <GraphView graph={p.data} />}
    </section>
  );
}
function GraphView({ graph }: { graph: Graph }) {
  const positions = new Map(
    graph.nodes.map((n, index) => [
      n.id,
      { x: 100 + n.depth * 220, y: 50 + index * 65 },
    ]),
  );
  const width = Math.max(440, ...graph.nodes.map((n) => 200 + n.depth * 220)),
    height = Math.max(130, graph.nodes.length * 65 + 30);
  return (
    <>
      {graph.truncated && (
        <PageState
          title="Graf został ograniczony"
          detail="Zwiększ limit lub zmień filtry, aby zobaczyć inne zapisane zależności."
        />
      )}
      <div className="cmdb-graph">
        <svg
          style={{ width, height }}
          viewBox={`0 0 ${width} ${height}`}
          role="img"
          aria-label="Graf zależności; lista węzłów i relacji znajduje się poniżej"
        >
          <defs>
            <marker
              id="cmdb-arrow"
              markerWidth="8"
              markerHeight="8"
              refX="7"
              refY="4"
              orient="auto"
            >
              <path d="M0,0 L8,4 L0,8 Z" fill="currentColor" />
            </marker>
          </defs>
          {graph.edges.map((e) => {
            const a = positions.get(e.source_id)!,
              b = positions.get(e.target_id)!;
            return (
              <line
                key={e.id}
                x1={a.x + Math.sign(b.x - a.x) * 85}
                y1={a.y + (a.x === b.x ? Math.sign(b.y - a.y) * 20 : 0)}
                x2={b.x - Math.sign(b.x - a.x) * 85}
                y2={b.y - (a.x === b.x ? Math.sign(b.y - a.y) * 20 : 0)}
                markerEnd="url(#cmdb-arrow)"
              />
            );
          })}
          {graph.nodes.map((n) => {
            const p = positions.get(n.id)!;
            return (
              <g key={n.id}>
                <rect
                  x={p.x - 85}
                  y={p.y - 20}
                  width="170"
                  height="40"
                  rx="5"
                />
                <text x={p.x} y={p.y + 5} textAnchor="middle">
                  {n.name.length > 20 ? n.name.slice(0, 19) + "…" : n.name}
                </text>
              </g>
            );
          })}
        </svg>
      </div>
      <ul className="cmdb-node-list">
        {graph.nodes.map((n) => (
          <li key={n.id}>
            <Link to={`/cmdb/${n.id}`}>{n.name}</Link> · {labels[n.class]}
            {n.archived ? " · Archiwum" : ""}
          </li>
        ))}
      </ul>
      <ul>
        {graph.edges.map((e) => (
          <li key={e.id}>
            {e.source_name} → {labels[e.type]} → {e.target_name}
          </li>
        ))}
      </ul>
    </>
  );
}

const inverse: Record<string, string> = {
  RUNS_ON: "Uruchamia",
  HOSTED_ON: "Hostuje",
  DEPENDS_ON: "Jest wymagany przez",
  PART_OF: "Zawiera",
  CONNECTS_TO: "Jest połączony z",
};
type Save = (path: string, body: unknown, method?: string) => Promise<boolean>;
export function RelationsPanel({
  record,
  busy,
  save,
}: {
  record: CI;
  busy: boolean;
  save: Save;
}) {
  const [choice, setChoice] = useState<Choice | null>(null),
    [kind, setKind] = useState("DEPENDS_ON"),
    [remove, setRemove] = useState<Relation>();
  const selected = useIncidentQuery<CI>(
    choice ? `/api/v1/cmdb/cis/${choice.id}` : null,
  );
  const targetID = remove
    ? remove.source_id === record.id
      ? remove.target_id
      : remove.source_id
    : undefined;
  const removing = useIncidentQuery<CI>(
    targetID ? `/api/v1/cmdb/cis/${targetID}` : null,
  );
  const p = useRemotePage<Relation>(
    `/api/v1/cmdb/cis/${record.id}/relations?revision=${record.version}`,
    "items",
    "before_id",
  );
  const { hasPermission } = useAuth();
  const manage =
    hasPermission("cmdb.manage") &&
    hasPermission("cmdb.read") &&
    !record.archived;
  return (
    <>
      <h3>Zależności</h3>
      <PageFeedback
        loading={p.loading}
        error={p.error}
        empty={!p.items.length}
      />
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Źródło</th>
              <th>Typ</th>
              <th>Cel</th>
              {manage && <th>Akcja</th>}
            </tr>
          </thead>
          <tbody>
            {p.items.map((e) => (
              <tr key={e.id}>
                <td>
                  <Link to={`/cmdb/${e.source_id}`}>{e.source_name}</Link>
                </td>
                <td>
                  {labels[e.type]}
                  {e.target_id === record.id
                    ? ` · Odwrotnie: ${inverse[e.type]}`
                    : ""}
                </td>
                <td>
                  <Link to={`/cmdb/${e.target_id}`}>{e.target_name}</Link>
                </td>
                {manage && (
                  <td>
                    <button disabled={busy} onClick={() => setRemove(e)}>
                      Usuń relację
                    </button>
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <More loading={p.loading} next={p.next} load={p.more} />
      {remove && (
        <div className="panel">
          <p>
            Usunąć zależność {remove.source_name} → {remove.target_name}?
          </p>
          <PageFeedback
            loading={removing.loading}
            error={removing.error}
            empty={false}
          />
          <button
            disabled={busy || !removing.data || removing.data.archived}
            onClick={async () => {
              if (
                await save(
                  `/api/v1/cmdb/cis/${record.id}/relations/${remove.id}`,
                  {
                    version: record.version,
                    target_version: removing.data!.version,
                  },
                  "DELETE",
                )
              )
                setRemove(undefined);
            }}
          >
            Potwierdź usunięcie
          </button>
          <button disabled={busy} onClick={() => setRemove(undefined)}>
            Anuluj
          </button>
        </div>
      )}
      {manage && (
        <form
          className="form-grid"
          onSubmit={async (e) => {
            e.preventDefault();
            if (!selected.data) return;
            if (
              await save(`/api/v1/cmdb/cis/${record.id}/relations`, {
                version: record.version,
                target_id: selected.data.id,
                target_version: selected.data.version,
                type: kind,
              })
            )
              setChoice(null);
          }}
        >
          <InventoryChoice
            label="Docelowy CI"
            value={choice}
            onChange={setChoice}
            endpoint="/api/v1/cmdb/cis?archived=false"
            disabled={busy}
          />
          <label>
            Typ zależności
            <select
              aria-label="Typ zależności"
              value={kind}
              onChange={(e) => setKind(e.target.value)}
              disabled={busy}
            >
              {[
                "RUNS_ON",
                "HOSTED_ON",
                "DEPENDS_ON",
                "CONNECTS_TO",
                "PART_OF",
              ].map((k) => (
                <option key={k} value={k}>
                  {labels[k]}
                </option>
              ))}
            </select>
          </label>
          {selected.data && (
            <p>
              Wybrany CI:{" "}
              <Link to={`/cmdb/${selected.data.id}`}>{selected.data.name}</Link>{" "}
              · {labels[selected.data.class]} · {labels[selected.data.status]} ·{" "}
              {labels[selected.data.environment]}
            </p>
          )}
          <PageFeedback
            loading={!!choice && selected.loading}
            error={selected.error}
            empty={false}
          />
          <button
            disabled={
              busy ||
              !selected.data ||
              selected.data.archived ||
              selected.data.id === record.id
            }
          >
            Dodaj zależność
          </button>
        </form>
      )}
      <DependencyGraph id={record.id} />
    </>
  );
}

export function LinkedRecords({ id }: { id: string }) {
  const { hasPermission } = useAuth();
  return (
    <>
      <h3>Powiązane zgłoszenia</h3>
      <p>
        Wyniki obejmują wyłącznie zgłoszenia, do których masz dostęp. Powiązania
        dodasz na szczegółach powiązanego rekordu.
      </p>
      {(hasPermission("incident.read") ||
        hasPermission("incident.read_own")) && (
        <RecordList id={id} kind="incidents" />
      )}
      {["problem.read", "problem.read_own"].some(hasPermission) && (
        <RecordList id={id} kind="problems" />
      )}
      {["change.read", "change.read_own"].some(hasPermission) && (
        <RecordList id={id} kind="changes" />
      )}
      {["request.read_own", "request.manage", "request.fulfill"].some(
        hasPermission,
      ) && <RecordList id={id} kind="requests" />}
    </>
  );
}
function RecordList({
  id,
  kind,
}: {
  id: string;
  kind: "incidents" | "requests" | "problems" | "changes";
}) {
  const p = useRemotePage<Reference>(
    `/api/v1/cmdb/cis/${id}/records/${kind}`,
    "items",
    "before_id",
  );
  return (
    <>
      <h4>
        {kind === "incidents"
          ? "Incydenty"
          : kind === "problems"
            ? "Problemy"
            : kind === "changes"
              ? "Zmiany"
              : "Wnioski"}
      </h4>
      <PageFeedback
        loading={p.loading}
        error={p.error}
        empty={!p.items.length}
      />
      <ul>
        {p.items.map((r) => (
          <li key={r.id}>
            <Link to={`/${kind}/${r.id}`}>
              {r.number} · {r.title}
            </Link>{" "}
            · {r.state}
          </li>
        ))}
      </ul>
      <More loading={p.loading} next={p.next} load={p.more} />
    </>
  );
}

export function RecordCIPanel({
  kind,
  id,
  onChanged,
  retain,
}: {
  kind: "incidents" | "requests" | "problems" | "changes";
  id: string;
  onChanged: () => void | Promise<void>;
  retain?: (tab: string, needed: boolean) => void;
}) {
  const { hasPermission } = useAuth();
  const allowed = hasPermission("cmdb.read") || hasPermission("cmdb.read_own");
  return allowed ? (
    <RecordCIEditor
      key={`${kind}:${id}`}
      kind={kind}
      id={id}
      onChanged={onChanged}
      retain={retain}
    />
  ) : (
    <PageState title="Brak uprawnień do CMDB" />
  );
}
function RecordCIEditor({
  kind,
  id,
  onChanged,
  retain,
}: {
  kind: "incidents" | "requests" | "problems" | "changes";
  id: string;
  onChanged: () => void | Promise<void>;
  retain?: (tab: string, needed: boolean) => void;
}) {
  const base = `/api/v1/cmdb/records/${kind}/${id}/cis`;
  const p = useIncidentQuery<{ record: Reference; items: CI[] }>(
    base + "?limit=100",
  );
  const [choice, setChoice] = useState<Choice | null>(null),
    [busy, setBusy] = useState(false),
    [error, setError] = useState<string>(),
    [notice, setNotice] = useState<string>(),
    [uncertain, setUncertain] = useState(false);
  useRetainedCommand(retain, "related_cis", busy || uncertain);
  const selected = useIncidentQuery<CI>(
    choice ? `/api/v1/cmdb/cis/${choice.id}` : null,
  );
  const operation = useRef<
    { path: string; method: string; body: string; key: string } | undefined
  >(undefined);
  const alive = useRef(true),
    pending = useRef(false),
    controller = useRef<AbortController | undefined>(undefined);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      controller.current?.abort();
    };
  }, []);
  const { hasPermission } = useAuth();
  async function change(ci: CI | undefined, remove: boolean) {
    if (pending.current || !p.data || (!ci && !operation.current)) return;
    pending.current = true;
    setBusy(true);
    setError(undefined);
    const c = new AbortController();
    controller.current = c;
    try {
      const proposal =
        uncertain && operation.current
          ? operation.current
          : {
              path: `${base}/${ci!.id}`,
              method: remove ? "DELETE" : "PUT",
              body: jsonBody({
                version: p.data.record.version,
                ci_version: ci!.version,
              }),
              key: crypto.randomUUID(),
            };
      operation.current = proposal;
      await api(proposal.path, {
        method: proposal.method,
        headers: { "Idempotency-Key": proposal.key },
        body: proposal.body,
        signal: c.signal,
      });
      setUncertain(false);
      operation.current = undefined;
      if (alive.current) {
        setChoice(null);
        setNotice("Zapisano powiązanie CI.");
        p.reload();
        void onChanged();
      }
    } catch (e) {
      if (alive.current && !c.signal.aborted) {
        setUncertain(!(e instanceof ApiError) || e.status >= 500);
        setError(text(e) + " Wybrany CI pozostaje w formularzu.");
      }
    } finally {
      pending.current = false;
      if (alive.current) setBusy(false);
    }
  }
  return (
    <section>
      <h3>Elementy konfiguracji (CI)</h3>
      <PageFeedback
        loading={p.loading}
        error={p.error}
        empty={!p.data?.items.length}
      />
      {error && (
        <PageState
          title="Nie zapisano powiązania"
          detail={error}
          tone="error"
        />
      )}
      {uncertain && (
        <p role="status">
          Nie potwierdzono powiązania. Ponowienie zachowa wybrany rekord, CI,
          treść i klucz operacji.{" "}
          <button disabled={busy} onClick={() => void change(undefined, false)}>
            Potwierdź wynik powiązania CI
          </button>
        </p>
      )}
      {notice && <PageState title={notice} tone="success" />}
      <button
        disabled={busy || uncertain || p.loading}
        onClick={() => {
          p.reload();
          selected.reload();
        }}
      >
        Odśwież powiązania
      </button>
      <ul>
        {p.data?.items.map((ci) => (
          <li key={ci.id}>
            <Link to={`/cmdb/${ci.id}`}>
              {ci.name} · {ci.identifier}
            </Link>{" "}
            · {labels[ci.class]}
            {ci.archived ? " · Archiwum" : ""}
            {hasPermission("cmdb.link") && p.data?.record.can_link && (
              <button
                disabled={busy || uncertain || p.loading}
                onClick={() => void change(ci, true)}
              >
                Odłącz CI
              </button>
            )}
          </li>
        ))}
      </ul>
      {hasPermission("cmdb.link") && p.data?.record.can_link && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (selected.data) void change(selected.data, false);
          }}
        >
          <InventoryChoice
            label="Element konfiguracji"
            value={choice}
            onChange={setChoice}
            endpoint="/api/v1/cmdb/cis?archived=false"
            disabled={busy || uncertain || p.loading}
          />
          <PageFeedback
            loading={!!choice && selected.loading}
            error={selected.error}
            empty={false}
          />
          {selected.data && (
            <p>
              Podgląd:{" "}
              <Link to={`/cmdb/${selected.data.id}`}>{selected.data.name}</Link>{" "}
              · {labels[selected.data.class]} · {labels[selected.data.status]} ·{" "}
              {labels[selected.data.environment]}
            </p>
          )}
          <button
            disabled={
              busy ||
              uncertain ||
              p.loading ||
              !selected.data ||
              selected.data.archived
            }
          >
            Powiąż CI
          </button>
        </form>
      )}
    </section>
  );
}
