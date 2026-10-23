import { useEffect, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { api, jsonBody } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { RemoteCombobox, type Choice } from "../components/RemoteCombobox";
import { useRemotePage } from "../components/useRemotePage";
import { PageState } from "../components/PageState";
import { useIncidentQuery } from "../incidents/hooks";
import { cmdbLabel, cmdbText, useCMDBLocale } from "./messages";
import type { Asset, CI, Graph, Metadata, Reference, Relation } from "./contracts";

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
  const locale = useCMDBLocale();
  return (
    <>
      {loading && <PageState title={cmdbText("loadingShared", locale)} />}
      {error && (
        <PageState
          title={cmdbText("loadSharedError", locale)}
          detail={error}
          tone="error"
        />
      )}
      {!loading && !error && empty && (
        <PageState title={cmdbText("emptyShared", locale)} />
      )}
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
  const locale = useCMDBLocale();
  return next ? (
    <button
      className="button button--secondary mt-3"
      type="button"
      disabled={loading}
      onClick={load}
    >
      {cmdbText("more", locale)}
    </button>
  ) : null;
}

export function InventoryHistory({ path }: { path: string }) {
  const locale = useCMDBLocale();
  type Entry = {
    id: string;
    actor_name: string;
    summary: string;
    event_time: string;
    data: unknown;
  };
  const page = useRemotePage<Entry>(path + "?limit=25", "entries", "before_id");
  const pendingProjection = Boolean(
    (page.response as { pending?: boolean } | undefined)?.pending,
  );

  return (
    <>
      <p>{cmdbText("historyHelp", locale)}</p>
      {pendingProjection && (
        <PageState title={cmdbText("pendingHistory", locale)} />
      )}
      <button
        className="button button--secondary"
        type="button"
        onClick={page.reload}
        disabled={page.loading}
      >
        {cmdbText("refreshHistory", locale)}
      </button>
      <PageFeedback
        loading={page.loading}
        error={page.error}
        empty={!page.items.length && !pendingProjection}
      />
      <ol className="timeline">
        {page.items.map((entry) => (
          <li key={entry.id}>
            <strong>{entry.summary}</strong>
            <p>
              {entry.actor_name} - {new Date(entry.event_time).toLocaleString(
                locale === "en" ? "en-GB" : "pl-PL",
              )}
            </p>
            <details>
              <summary>{cmdbText("historyDetails", locale)}</summary>
              <pre>{JSON.stringify(entry.data, null, 2)}</pre>
            </details>
          </li>
        ))}
      </ol>
      <More loading={page.loading} next={page.next} load={page.more} />
    </>
  );
}

export function DependencyGraph({
  id,
  metadata,
}: {
  id: string;
  metadata: Metadata;
}) {
  const locale = useCMDBLocale();
  const [params, setParams] = useSearchParams();
  const get = (key: string, fallback: string) => params.get(key) ?? fallback;
  const change = (key: string, value: string) => {
    const next = new URLSearchParams(params);
    value ? next.set(key, value) : next.delete(key);
    setParams(next, { replace: true });
  };
  const query = new URLSearchParams({
    depth: get("depth", "2"),
    max_nodes: get("max_nodes", "50"),
    type: get("relation_type", ""),
    class: get("graph_class", ""),
    environment: get("graph_environment", ""),
  });
  const graph = useIncidentQuery<Graph>(
    `/api/v1/cmdb/cis/${id}/graph?${query.toString()}`,
  );

  return (
    <section className="mt-5">
      <h3>{cmdbText("graphTitle", locale)}</h3>
      <p>{cmdbText("graphDescription", locale)}</p>
      <div className="filter-bar">
        <label>
          {cmdbText("depth", locale)}
          <input
            type="number"
            min="0"
            max="5"
            value={get("depth", "2")}
            onChange={(event) => change("depth", event.target.value)}
          />
        </label>
        <label>
          {cmdbText("nodeLimit", locale)}
          <input
            type="number"
            min="1"
            max="100"
            value={get("max_nodes", "50")}
            onChange={(event) => change("max_nodes", event.target.value)}
          />
        </label>
        <label>
          {cmdbText("relationAction", locale)}
          <select
            value={get("relation_type", "")}
            onChange={(event) => change("relation_type", event.target.value)}
          >
            <option value="">{cmdbText("all", locale)}</option>
            {metadata.relation_types.map((item) => (
              <option key={item.key} value={item.key}>
                {cmdbLabel(item.key, locale)}
              </option>
            ))}
          </select>
        </label>
        <label>
          {cmdbText("graphClass", locale)}
          <select
            value={get("graph_class", "")}
            onChange={(event) => change("graph_class", event.target.value)}
          >
            <option value="">{cmdbText("all", locale)}</option>
            {metadata.classes.map((item) => (
              <option key={item.key} value={item.key}>
                {cmdbLabel(item.key, locale)}
              </option>
            ))}
          </select>
        </label>
        <label>
          {cmdbText("graphEnvironment", locale)}
          <select
            value={get("graph_environment", "")}
            onChange={(event) => change("graph_environment", event.target.value)}
          >
            <option value="">{cmdbText("all", locale)}</option>
            {metadata.environments.map((item) => (
              <option key={item} value={item}>
                {cmdbLabel(item, locale)}
              </option>
            ))}
          </select>
        </label>
      </div>
      <PageFeedback
        loading={graph.loading}
        error={graph.error}
        empty={false}
      />
      {graph.data && (
        <GraphView graph={graph.data} returnTo={params.get("return")} />
      )}
    </section>
  );
}

function GraphView({
  graph,
  returnTo,
}: {
  graph: Graph;
  returnTo: string | null;
}) {
  const locale = useCMDBLocale();
  const positions = new Map(
    graph.nodes.map((node, index) => [
      node.id,
      { x: 100 + node.depth * 220, y: 50 + index * 65 },
    ]),
  );
  const width = Math.max(440, ...graph.nodes.map((node) => 120 + node.depth * 220));
  const height = Math.max(130, graph.nodes.length * 65 + 30);
  const nodeLink = (nodeID: string) =>
    "/cmdb/" + nodeID + (returnTo ? "?return=" + encodeURIComponent(returnTo) : "");

  return (
    <>
      {graph.truncated && (
        <PageState
          title={cmdbText("graphTruncated", locale)}
          detail={cmdbText("graphTruncatedDetail", locale)}
        />
      )}
      <div className="cmdb-graph">
        <svg
          viewBox={`0 0 ${width} ${height}`}
          role="img"
          aria-label={cmdbText("graphTitle", locale)}
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
          {graph.edges.map((edge) => {
            const from = positions.get(edge.source_id)!;
            const to = positions.get(edge.target_id)!;
            return (
              <line
                key={edge.id}
                x1={from.x}
                y1={from.y}
                x2={to.x}
                y2={to.y}
                markerEnd="url(#cmdb-arrow)"
              />
            );
          })}
          {graph.nodes.map((node) => {
            const position = positions.get(node.id)!;
            return (
              <g key={node.id}>
                <rect
                  x={position.x - 85}
                  y={position.y - 20}
                  width="170"
                  height="40"
                  rx="5"
                />
                <text x={position.x} y={position.y + 5} textAnchor="middle">
                  {node.name.length > 20
                    ? node.name.slice(0, 19) + "…"
                    : node.name}
                </text>
              </g>
            );
          })}
        </svg>
      </div>
      <h4 className="mt-4">{cmdbText("nodes", locale)}</h4>
      <ul className="cmdb-node-list">
        {graph.nodes.map((node) => (
          <li key={node.id}>
            <Link to={nodeLink(node.id)}>{node.name}</Link>
            <span> - {cmdbLabel(node.class, locale)}</span>
            {node.archived && (
              <span> - {cmdbText("archivedBadge", locale)}</span>
            )}
          </li>
        ))}
      </ul>
      <h4 className="mt-4">{cmdbText("relations", locale)}</h4>
      <ul className="cmdb-edge-list">
        {graph.edges.map((edge) => (
          <li key={edge.id}>
            <Link to={nodeLink(edge.source_id)}>{edge.source_name}</Link>
            <span> - {cmdbLabel(edge.type, locale)} - </span>
            <Link to={nodeLink(edge.target_id)}>{edge.target_name}</Link>
          </li>
        ))}
      </ul>
    </>
  );
}

type Save = (path: string, body: unknown, method?: string) => Promise<boolean>;

export function RelationsPanel({
  record,
  metadata,
  busy,
  save,
}: {
  record: CI;
  metadata: Metadata;
  busy: boolean;
  save: Save;
}) {
  const locale = useCMDBLocale();
  const [params] = useSearchParams();
  const returnTo = params.get("return");
  const [choice, setChoice] = useState<Choice | null>(null);
  const [kind, setKind] = useState("");
  const [remove, setRemove] = useState<Relation>();
  const selected = useIncidentQuery<CI>(
    choice ? "/api/v1/cmdb/cis/" + choice.id : null,
  );
  const targetID = remove
    ? remove.source_id === record.id
      ? remove.target_id
      : remove.source_id
    : undefined;
  const removing = useIncidentQuery<CI>(
    targetID ? "/api/v1/cmdb/cis/" + targetID : null,
  );
  const page = useRemotePage<Relation>(
    `/api/v1/cmdb/cis/${record.id}/relations?revision=${record.version}`,
    "items",
    "before_id",
  );
  const { hasPermission } = useAuth();
  const manage =
    hasPermission("cmdb.manage") &&
    hasPermission("cmdb.read") &&
    !record.archived;
  const allowedTypes = metadata.relation_types.filter((definition) =>
    definition.pairs.some(
      (pair) =>
        pair.source === record.class && pair.target === selected.data?.class,
    ),
  );
  const activeKind = allowedTypes.some((definition) => definition.key === kind)
    ? kind
    : "";
  const recordLink = (id: string) =>
    "/cmdb/" + id + (returnTo ? "?return=" + encodeURIComponent(returnTo) : "");

  return (
    <section>
      <h3>{cmdbText("relations", locale)}</h3>
      {record.archived && (
        <PageState title={cmdbText("archiveReadOnly", locale)} />
      )}
      {!manage && !record.archived && (
        <p className="muted">{cmdbText("relationManageDenied", locale)}</p>
      )}
      <PageFeedback
        loading={page.loading}
        error={page.error}
        empty={!page.items.length}
      />
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>{cmdbText("source", locale)}</th>
              <th>{cmdbText("relationAction", locale)}</th>
              <th>{cmdbText("graphTarget", locale)}</th>
              {manage && <th>{cmdbText("relationManageAction", locale)}</th>}
            </tr>
          </thead>
          <tbody>
            {page.items.map((edge) => (
              <tr key={edge.id}>
                <td>
                  <Link to={recordLink(edge.source_id)}>{edge.source_name}</Link>
                </td>
                <td>{cmdbLabel(edge.type, locale)}</td>
                <td>
                  <Link to={recordLink(edge.target_id)}>{edge.target_name}</Link>
                </td>
                {manage && (
                  <td>
                    <button
                      className="button button--secondary"
                      type="button"
                      disabled={busy}
                      onClick={() => setRemove(edge)}
                    >
                      {cmdbText("removeRelation", locale)}
                    </button>
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <More loading={page.loading} next={page.next} load={page.more} />
      {remove && (
        <div className="panel mt-4" role="group" aria-label={cmdbText("removeRelation", locale)}>
          <p>
            {cmdbText("removeRelationPrompt", locale)}: {remove.source_name} -{" "}
            {remove.target_name}?
          </p>
          <PageFeedback
            loading={removing.loading}
            error={removing.error}
            empty={false}
          />
          <div className="action-row">
            <button
              className="button button--danger"
              type="button"
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
                ) {
                  setRemove(undefined);
                }
              }}
            >
              {cmdbText("confirmRemoveRelation", locale)}
            </button>
            <button
              className="button button--secondary"
              type="button"
              disabled={busy}
              onClick={() => setRemove(undefined)}
            >
              {cmdbText("cancel", locale)}
            </button>
          </div>
        </div>
      )}
      {manage && (
        <form
          className="form-grid mt-4"
          onSubmit={async (event) => {
            event.preventDefault();
            if (!selected.data || !activeKind) return;
            if (
              await save(`/api/v1/cmdb/cis/${record.id}/relations`, {
                version: record.version,
                target_id: selected.data.id,
                target_version: selected.data.version,
                type: activeKind,
              })
            ) {
              setChoice(null);
              setKind("");
            }
          }}
        >
          <InventoryChoice
            label={cmdbText("targetCISelect", locale)}
            value={choice}
            onChange={setChoice}
            endpoint="/api/v1/cmdb/cis?archived=false"
            disabled={busy}
          />
          <label>
            {cmdbText("relationAction", locale)}
            <select
              value={activeKind}
              onChange={(event) => setKind(event.target.value)}
              disabled={busy || !selected.data}
            >
              <option value="">{cmdbText("select", locale)}</option>
              {allowedTypes.map((definition) => (
                <option key={definition.key} value={definition.key}>
                  {cmdbLabel(definition.key, locale)}
                </option>
              ))}
            </select>
          </label>
          {selected.data && (
            <p>
              {cmdbLabel(record.class, locale)} -{" "}
              {cmdbLabel(selected.data.class, locale)}. {cmdbText(
                "allowedPairHint",
                locale,
              )}
            </p>
          )}
          {selected.data && allowedTypes.length === 0 && (
            <PageState title={cmdbText("noAllowedRelation", locale)} />
          )}
          <PageFeedback
            loading={Boolean(choice) && selected.loading}
            error={selected.error}
            empty={false}
          />
          <button
            className="button button--primary"
            disabled={
              busy ||
              !selected.data ||
              selected.data.archived ||
              selected.data.id === record.id ||
              !activeKind
            }
          >
            {cmdbText("addRelation", locale)}
          </button>
        </form>
      )}
      <DependencyGraph id={record.id} metadata={metadata} />
    </section>
  );
}

export function AssetPanel({
  record,
  busy,
  save,
}: {
  record: CI;
  busy: boolean;
  save: Save;
}) {
  const locale = useCMDBLocale();
  const { hasPermission } = useAuth();
  const canReadAsset =
    hasPermission("assets.read") || hasPermission("assets.read_own");
  const link = useIncidentQuery<{ asset?: Asset }>(
    canReadAsset ? `/api/v1/cmdb/cis/${record.id}/asset` : null,
    record.version,
  );
  const [choice, setChoice] = useState<Choice | null>(null);
  const selected = useIncidentQuery<Asset>(
    choice ? "/api/v1/cmdb/assets/" + choice.id : null,
  );
  const manage =
    hasPermission("cmdb.manage") &&
    hasPermission("cmdb.read") &&
    hasPermission("assets.manage") &&
    hasPermission("assets.read") &&
    !record.archived;

  return (
    <section>
      <h3>{cmdbText("assetTitle", locale)}</h3>
      {!canReadAsset && (
        <PageState title={cmdbText("assetHidden", locale)} />
      )}
      {canReadAsset && (
        <PageFeedback loading={link.loading} error={link.error} empty={false} />
      )}
      {record.archived && (
        <PageState title={cmdbText("archiveReadOnly", locale)} />
      )}
      {link.data?.asset ? (
        <div className="panel">
          <Link to={`/assets/${link.data.asset.id}`}>
            {link.data.asset.name} - {link.data.asset.asset_tag}
          </Link>
          <p>
            {cmdbText("serialNumber", locale)}: {link.data.asset.serial_number || "-"} -{" "}
            {cmdbLabel(link.data.asset.state, locale)}
          </p>
          {!manage && !record.archived && (
            <p className="muted">
              {cmdbText("assetLinkManageDenied", locale)}
            </p>
          )}
          {manage && (
            <button
              className="button button--secondary"
              type="button"
              disabled={busy || link.data.asset.archived}
              onClick={() =>
                void save(`/api/v1/cmdb/cis/${record.id}/asset`, {
                  version: record.version,
                  asset_id: "0",
                  asset_version: link.data!.asset!.version,
                }, "PUT")
              }
            >
              {cmdbText("unlinkAsset", locale)}
            </button>
          )}
          {link.data.asset.archived && (
            <p className="muted">
              {cmdbText("archivedAssetUnlink", locale)}
            </p>
          )}
        </div>
      ) : (
        canReadAsset &&
        !link.loading &&
        !link.error && (
          <PageState
            title={cmdbText("noVisibleAssetLink", locale)}
            detail={cmdbText("assetLinkVisibility", locale)}
          />
        )
      )}
      {manage && !link.data?.asset && (
        <form
          className="form-grid mt-4"
          onSubmit={async (event) => {
            event.preventDefault();
            if (
              selected.data &&
              (await save(`/api/v1/cmdb/cis/${record.id}/asset`, {
                version: record.version,
                asset_id: selected.data.id,
                asset_version: selected.data.version,
              }, "PUT"))
            ) {
              setChoice(null);
            }
          }}
        >
          <InventoryChoice
            label={cmdbText("assetPickerLabel", locale)}
            value={choice}
            onChange={setChoice}
            endpoint="/api/v1/cmdb/assets?archived=false"
            disabled={busy}
          />
          <PageFeedback
            loading={Boolean(choice) && selected.loading}
            error={selected.error}
            empty={false}
          />
          <button
            className="button button--primary"
            disabled={busy || !selected.data || selected.data.archived}
          >
            {cmdbText("linkAsset", locale)}
          </button>
        </form>
      )}
      {!manage &&
        !record.archived &&
        canReadAsset &&
        !link.data?.asset && (
          <p className="muted">
            {cmdbText("assetCreateManageDenied", locale)}
          </p>
        )}
    </section>
  );
}

export function LinkedRecords({ id }: { id: string }) {
  const locale = useCMDBLocale();
  const { hasPermission } = useAuth();
  const canReadIncidents =
    hasPermission("incident.read") || hasPermission("incident.read_own");
  const canReadRequests = [
    "request.read_own",
    "request.manage",
    "request.fulfill",
  ].some(hasPermission);
  return (
    <section>
      <h3>{cmdbText("relatedRecords", locale)}</h3>
      <p>{cmdbText("relatedRecordsScope", locale)}</p>
      {canReadIncidents && <RecordList id={id} kind="incidents" />}
      {canReadRequests && <RecordList id={id} kind="requests" />}
      {!canReadIncidents && !canReadRequests && (
        <PageState title={cmdbText("noLinkedRecordsPermission", locale)} />
      )}
    </section>
  );
}

function RecordList({
  id,
  kind,
}: {
  id: string;
  kind: "incidents" | "requests";
}) {
  const locale = useCMDBLocale();
  const page = useRemotePage<Reference>(
    `/api/v1/cmdb/cis/${id}/records/${kind}`,
    "items",
    "before_id",
  );
  return (
    <section className="panel mt-4">
      <h4>{cmdbText(kind, locale)}</h4>
      <PageFeedback
        loading={page.loading}
        error={page.error}
        empty={!page.items.length}
      />
      <ul>
        {page.items.map((record) => (
          <li key={record.id}>
            <Link to={`/${kind}/${record.id}`}>
              {record.number} - {record.title}
            </Link>
            <span> - {record.state}</span>
          </li>
        ))}
      </ul>
      <More loading={page.loading} next={page.next} load={page.more} />
    </section>
  );
}

export function RecordCIPanel({
  kind,
  id,
  onChanged,
}: {
  kind: "incidents" | "requests";
  id: string;
  onChanged: () => void | Promise<void>;
}) {
  const { hasPermission } = useAuth();
  const allowed =
    hasPermission("cmdb.read") || hasPermission("cmdb.read_own");
  return allowed ? (
    <RecordCIEditor
      key={`${kind}:${id}`}
      kind={kind}
      id={id}
      onChanged={onChanged}
    />
  ) : (
    <PageState title={cmdbText("cmdbLinkPermission")} />
  );
}

function RecordCIEditor({
  kind,
  id,
  onChanged,
}: {
  kind: "incidents" | "requests";
  id: string;
  onChanged: () => void | Promise<void>;
}) {
  const locale = useCMDBLocale();
  const base = `/api/v1/cmdb/records/${kind}/${id}/cis`;
  const page = useIncidentQuery<{ record: Reference; items: CI[] }>(
    base + "?limit=100",
  );
  const [choice, setChoice] = useState<Choice | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [notice, setNotice] = useState<string>();
  const selected = useIncidentQuery<CI>(
    choice ? `/api/v1/cmdb/cis/${choice.id}` : null,
  );
  const alive = useRef(true);
  const pending = useRef(false);
  const controller = useRef<AbortController | undefined>(undefined);
  const { hasPermission } = useAuth();

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      controller.current?.abort();
    };
  }, []);

  async function change(record: CI, remove: boolean) {
    if (pending.current || !page.data) return;
    pending.current = true;
    setBusy(true);
    setError(undefined);
    const request = new AbortController();
    controller.current = request;
    try {
      await api(`${base}/${record.id}`, {
        method: remove ? "DELETE" : "PUT",
        body: jsonBody({
          version: page.data.record.version,
          ci_version: record.version,
        }),
        signal: request.signal,
      });
      if (alive.current) {
        setChoice(null);
        setNotice(cmdbText("savedRelation", locale));
        page.reload();
        void Promise.resolve(onChanged()).catch(() => undefined);
      }
    } catch (cause) {
      if (alive.current && !request.signal.aborted) {
        const detail = cause instanceof Error ? cause.message : String(cause);
        setError(
          `${detail} ${cmdbText("currentStateBeforeRetry", locale)}`,
        );
      }
    } finally {
      pending.current = false;
      if (alive.current) setBusy(false);
    }
  }

  return (
    <section className="panel">
      <h3>{cmdbText("relatedCIs", locale)}</h3>
      <PageFeedback
        loading={page.loading}
        error={page.error}
        empty={!page.data?.items.length}
      />
      {error && (
        <PageState
          title={cmdbText("saveRelationFailed", locale)}
          detail={error}
          tone="error"
        />
      )}
      {notice && <PageState title={notice} tone="success" />}
      <button
        className="button button--secondary"
        type="button"
        disabled={busy || page.loading}
        onClick={() => page.reload()}
      >
        {cmdbText("refreshRelations", locale)}
      </button>
      <ul className="mt-3">
        {page.data?.items.map((record) => (
          <li key={record.id}>
            <Link to={`/cmdb/${record.id}`}>
              {record.name} - {record.identifier}
            </Link>
            <span> - {cmdbLabel(record.class, locale)}</span>
            {record.archived && (
              <span> - {cmdbText("archivedBadge", locale)}</span>
            )}
            {hasPermission("cmdb.link") && page.data?.record.can_link && (
              <button
                className="button button--secondary ml-2"
                type="button"
                disabled={busy || page.loading}
                onClick={() => void change(record, true)}
              >
                {cmdbText("unlinkCI", locale)}
              </button>
            )}
          </li>
        ))}
      </ul>
      {hasPermission("cmdb.link") && page.data?.record.can_link && (
        <form
          className="form-grid mt-4"
          onSubmit={(event) => {
            event.preventDefault();
            if (selected.data) void change(selected.data, false);
          }}
        >
          <InventoryChoice
            label={cmdbText("linkCI", locale)}
            value={choice}
            onChange={setChoice}
            endpoint="/api/v1/cmdb/cis?archived=false"
            disabled={busy || page.loading}
          />
          <PageFeedback
            loading={Boolean(choice) && selected.loading}
            error={selected.error}
            empty={false}
          />
          <button
            className="button button--primary"
            disabled={
              busy || page.loading || !selected.data || selected.data.archived
            }
          >
            {cmdbText("linkCIButton", locale)}
          </button>
        </form>
      )}
    </section>
  );
}
