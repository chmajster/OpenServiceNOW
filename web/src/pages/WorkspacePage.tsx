import { useIncidentQuery } from "../incidents/hooks";
import type { Metadata as IncidentMetadata } from "../incidents/contracts";
import { majorMessages } from "../incidents/majorMessages";
import { useCMDBLocale } from "../cmdb/messages";
import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { PageState } from "../components/PageState";
import { RemoteCombobox, type Choice } from "../components/RemoteCombobox";
import { UnsavedGuard } from "../incidents/UnsavedGuard";
import {
  deleteWorkView,
  getWorkExport,
  getWorkItem,
  listSavedViews,
  listWorkFields,
  queryWork,
  requestWorkExport,
  runWorkAction,
  saveWorkView,
} from "../workspace/api";
import type {
  ActionResult,
  ExportStatus,
  FieldDefinition,
  FilterNode,
  SavedView,
  WorkItem,
  WorkQuery,
  WorkReference,
} from "../workspace/contracts";
import { WorkRecordEditor } from "../workspace/WorkRecordEditor";

type SavedReference = WorkReference & { key: string; number: string; title: string; record_path: string };
type LocalState = { tabs: SavedReference[]; recent: SavedReference[]; favorites: SavedReference[]; query?: Partial<WorkQuery> };
type FilterConditionDraft = { kind: "condition"; field: string; compare: string; value: string };
type FilterGroupDraft = { kind: "group"; operator: "AND" | "OR"; children: FilterDraftNode[] };
type FilterDraftNode = FilterConditionDraft | FilterGroupDraft;

const initialColumns = ["number", "object_type", "title", "state", "priority", "assigned_to", "assignment_group_id", "updated_at"];
const states = ["NEW", "OPEN", "IN_PROGRESS", "ON_HOLD", "RESOLVED", "COMPLETED", "CLOSED", "CANCELLED"];

function safeReferences(value: unknown, tenantID: string): SavedReference[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((entry): SavedReference[] => {
    if (!entry || typeof entry !== "object") return [];
    const candidate = entry as Partial<SavedReference>;
    if (
      candidate.tenant_id !== tenantID ||
      (candidate.object_type !== "incident" && candidate.object_type !== "fulfillment_task" && candidate.object_type !== "incident_action") ||
      typeof candidate.object_id !== "string" ||
      !/^[1-9][0-9]{0,19}$/.test(candidate.object_id) ||
      typeof candidate.number !== "string" ||
      typeof candidate.title !== "string"
    ) return [];
    const reference = {
      tenant_id: tenantID,
      object_type: candidate.object_type,
      object_id: candidate.object_id,
    };
    return [{
      ...reference,
      key: referenceKey(reference),
      number: candidate.number.slice(0, 80),
      title: candidate.title.slice(0, 500),
      record_path: recordPath(reference),
    }];
  });
}

function safeFilterNode(value: unknown, depth = 0, budget = { count: 0 }): FilterNode | null {
  if (!value || typeof value !== "object" || depth > 5 || budget.count >= 40) return null;
  budget.count += 1;
  const node = value as Record<string, unknown>;
  if (Array.isArray(node.children)) {
    if ((node.operator !== "AND" && node.operator !== "OR") || node.children.length < 1 || node.children.length > 20) return null;
    const children = node.children.map((child) => safeFilterNode(child, depth + 1, budget));
    if (children.some((child) => child === null)) return null;
    return { operator: node.operator, children: children as FilterNode[] };
  }
  const raw = node.value;
  const validValue = (typeof raw === "string" && raw.length <= 5000) || (typeof raw === "number" && Number.isFinite(raw)) || typeof raw === "boolean" || (Array.isArray(raw) && raw.length > 0 && raw.length <= 20 && raw.every((item) => (typeof item === "string" && item.length <= 500) || (typeof item === "number" && Number.isFinite(item))));
  if (typeof node.field !== "string" || node.field.length > 100 || typeof node.compare !== "string" || node.compare.length > 40 || !validValue) return null;
  return { field: node.field, compare: node.compare, value: raw as FilterNode["value"] };
}

function safeQuery(value: unknown): Partial<WorkQuery> | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
  const candidate = value as Record<string, unknown>;
  const views: WorkQuery["view"][] = ["mine", "group", "unassigned", "watched", "all"];
  const sorts: WorkQuery["sort"][] = ["updated_desc", "created_desc", "priority_asc", "number_asc", "rank"];
  const safe: Partial<WorkQuery> = {};
  if (typeof candidate.view === "string" && views.includes(candidate.view as WorkQuery["view"])) safe.view = candidate.view as WorkQuery["view"];
  if (typeof candidate.sort === "string" && sorts.includes(candidate.sort as WorkQuery["sort"])) safe.sort = candidate.sort as WorkQuery["sort"];
  if (typeof candidate.group_by === "string" && ["", "object_type", "state", "state_category", "priority", "assignment_group_id"].includes(candidate.group_by)) safe.group_by = candidate.group_by;
  if (Array.isArray(candidate.types)) safe.types = candidate.types.filter((type): type is WorkReference["object_type"] => type === "incident" || type === "fulfillment_task" || type === "incident_action");
  const safeColumns = (input: unknown) => Array.isArray(input) ? input.filter((column): column is string => typeof column === "string" && column.length <= 100).slice(0, 100) : undefined;
  const columns = safeColumns(candidate.columns);
  const order = safeColumns(candidate.column_order);
  if (columns) safe.columns = columns;
  if (order) safe.column_order = order;
  if (candidate.column_widths && typeof candidate.column_widths === "object" && !Array.isArray(candidate.column_widths)) {
    const widths: Record<string, number> = {};
    for (const [key, width] of Object.entries(candidate.column_widths)) if (key.length <= 100 && typeof width === "number" && Number.isFinite(width)) widths[key] = Math.max(80, Math.min(800, width));
    safe.column_widths = widths;
  }
  if (typeof candidate.limit === "number" && [25, 50, 100].includes(candidate.limit)) safe.limit = candidate.limit;
  if (candidate.filters !== undefined) safe.filters = safeFilterNode(candidate.filters) ?? undefined;
  return safe;
}

function pruneFilterDrafts(nodes: FilterDraftNode[], allowedFields: Set<string>): FilterDraftNode[] {
  return nodes.flatMap<FilterDraftNode>((node) => {
    if (node.kind === "condition") return allowedFields.has(node.field) ? [node] : [];
    const children = pruneFilterDrafts(node.children, allowedFields);
    return children.length ? [{ ...node, children }] : [];
  });
}

function safeRead(key: string, tenantID: string): LocalState {
  try {
    const value = localStorage.getItem(key);
    if (!value) return { tabs: [], recent: [], favorites: [] };
    const parsed = JSON.parse(value) as LocalState;
    return {
      tabs: safeReferences(parsed.tabs, tenantID).slice(0, 8),
      recent: safeReferences(parsed.recent, tenantID).slice(0, 10),
      favorites: safeReferences(parsed.favorites, tenantID).slice(0, 20),
      query: safeQuery(parsed.query),
    };
  } catch {
    return { tabs: [], recent: [], favorites: [] };
  }
}

function defaultQuery(types: WorkQuery["types"]): WorkQuery {
  return { types, view: "mine", sort: "updated_desc", columns: initialColumns, column_order: initialColumns, column_widths: { number: 140, object_type: 140, title: 320, state: 140, priority: 100, assigned_to: 180, assignment_group_id: 180, updated_at: 180 }, limit: 25 };
}

function referenceKey(ref: WorkReference) {
  return `${ref.tenant_id}:${ref.object_type}:${ref.object_id}`;
}

function recordPath(ref: WorkReference) {
  const segment = encodeURIComponent(ref.object_id);
  if(ref.object_type==="incident_action")return `/workspace/major?view=actions&action_id=${segment}`;
 return ref.object_type === "incident" ? `/incidents/${segment}` : `/fulfillment/${segment}`;
}

function stateLabel(state: string) {
  return ({ NEW: "Nowy", OPEN: "Otwarte", IN_PROGRESS: "W trakcie", ON_HOLD: "Wstrzymane", RESOLVED: "Rozwiązane", COMPLETED: "Ukończone", CLOSED: "Zamknięte", CANCELLED: "Anulowane" } as Record<string, string>)[state] ?? state;
}

export function WorkspacePage() {
  const module08Text=majorMessages[useCMDBLocale()];
  const { session, hasPermission } = useAuth();
  const incidentOptions=useIncidentQuery<IncidentMetadata>(hasPermission("incident.read")||hasPermission("incident.read_own")?"/api/v1/incidents/metadata":null);
  const [holdReason,setHoldReason]=useState("");

  const [urlParams] = useSearchParams();
  const tenantID = session?.tenant.id ?? "0";
  const userID = session?.user.id ?? "0";
  const storageKey = `opensnow-workspace:${tenantID}:${userID}`;
  const canIncidents = hasPermission("incident.read") || hasPermission("incident.read_own");
  const canTasks = hasPermission("request.fulfill") || hasPermission("request.manage");
  const canShare = hasPermission("admin.rbac.manage");
  const allowedTypes = useMemo<NonNullable<WorkQuery["types"]>>(() => [
    ...(canIncidents ? (["incident","incident_action"] as const) : []),
    ...(canTasks ? (["fulfillment_task"] as const) : []),
  ], [canIncidents, canTasks]);
  const savedLocal = useMemo(() => safeRead(storageKey, tenantID), [storageKey, tenantID]);
  const initialSavedFilters = savedLocal.query?.filters;
  const [baseQuery, setBaseQuery] = useState<WorkQuery>(() => ({ ...defaultQuery(allowedTypes), query: urlParams.get("q") ?? "", ...savedLocal.query }));
  const [filterRows, setFilterRows] = useState<FilterDraftNode[]>(() => (initialSavedFilters?.children ?? []).map(filterDraftFromNode).filter((node):node is FilterDraftNode=>node!==null));
  const [filterLogic, setFilterLogic] = useState<"AND" | "OR">(() => initialSavedFilters?.operator === "OR" ? "OR" : "AND");
  const [fields, setFields] = useState<FieldDefinition[]>([]);
  const [fieldsReady, setFieldsReady] = useState(false);
  const [views, setViews] = useState<SavedView[]>([]);
  const [viewsError, setViewsError] = useState("");
  const [items, setItems] = useState<WorkItem[]>([]);
  const [nextCursor, setNextCursor] = useState("");
  const [moreBusy, setMoreBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [refreshKey, setRefreshKey] = useState(0);
  const [tabs, setTabs] = useState<SavedReference[]>(savedLocal.tabs);
  const [mountedEditors, setMountedEditors] = useState<string[]>([]);
  const [editorDirty, setEditorDirty] = useState<Record<string, boolean>>({});
  const [pendingClose, setPendingClose] = useState("");
  const closeDialogFirst = useRef<HTMLButtonElement>(null);
  const [recent, setRecent] = useState<SavedReference[]>(savedLocal.recent);
  const [favorites, setFavorites] = useState<SavedReference[]>(savedLocal.favorites);
  const [selectedKey, setSelectedKey] = useState("");
  const [detail, setDetail] = useState<WorkItem | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState("");
  const [selectedRows, setSelectedRows] = useState<string[]>([]);
  const [selectionError, setSelectionError] = useState("");
  const [bulkAction, setBulkAction] = useState<"assign" | "transition">("assign");
  const [bulkState, setBulkState] = useState("IN_PROGRESS");
  const [reason, setReason] = useState("");
  const [resolutionCode, setResolutionCode] = useState("FIXED");
  useEffect(()=>{const active=incidentOptions.data?.resolution_codes.filter(o=>!o.retired);if(active&&!active.some(o=>o.value===resolutionCode))setResolutionCode(active[0]?.value??"")},[incidentOptions.data,resolutionCode]);
  const [resolutionDescription, setResolutionDescription] = useState("");
  const [results, setResults] = useState<ActionResult[]>([]);
  const [actionBusy, setActionBusy] = useState(false);
  const [viewName, setViewName] = useState("");
  const [shareView, setShareView] = useState(false);
  const [chosenView, setChosenView] = useState("");
  const [exportStatus, setExportStatus] = useState<ExportStatus | null>(null);
  const [exportError, setExportError] = useState("");
  const [exportBusy, setExportBusy] = useState(false);
  const [exportCheckBusy, setExportCheckBusy] = useState(false);
  const exportPollAttempts = useRef(0);
  const [inlineStates, setInlineStates] = useState<Record<string, string>>({});
  const reportEditorDirty = useCallback((key: string, dirty: boolean) => {
    setEditorDirty((current) => current[key] === dirty ? current : { ...current, [key]: dirty });
  }, []);

  const requestQuery = useMemo<WorkQuery>(() => {
    const active = filterRows.map((node)=>filterDraftToNode(node,fields)).filter((node):node is FilterNode=>node!==null);
    return {
      ...baseQuery,
      types: baseQuery.types?.filter((type) => allowedTypes.includes(type)),
      filters: active.length ? { operator: filterLogic, children: active } : undefined,
      cursor: undefined,
    };
  }, [baseQuery, filterRows, filterLogic, fields, allowedTypes]);

  useEffect(() => {
    const controller = new AbortController();
    void Promise.allSettled([listWorkFields(controller.signal), listSavedViews(controller.signal)])
      .then(([fieldResult, viewResult]) => {
        if (controller.signal.aborted) return;
        const allowedFields = new Set(initialColumns);
        if (fieldResult.status === "fulfilled") {
          setFields(fieldResult.value.fields);
          for (const field of fieldResult.value.fields) allowedFields.add(field.key);
        }
        setFilterRows((current) => pruneFilterDrafts(current, allowedFields));
        setBaseQuery((current) => ({
          ...current,
          columns: current.columns?.filter((key) => allowedFields.has(key)),
          column_order: current.column_order?.filter((key) => allowedFields.has(key)),
          column_widths: Object.fromEntries(Object.entries(current.column_widths ?? {}).filter(([key]) => allowedFields.has(key))),
        }));
        if (viewResult.status === "fulfilled") setViews(viewResult.value.items);
        if (fieldResult.status === "rejected" || viewResult.status === "rejected") {
          const cause = fieldResult.status === "rejected" ? fieldResult.reason : viewResult.status === "rejected" ? viewResult.reason : undefined;
          setViewsError(cause instanceof Error ? cause.message : "Nie udało się wczytać konfiguracji workspace.");
        } else setViewsError("");
        setFieldsReady(true);
      });
    return () => controller.abort();
  }, [tenantID, userID]);

  useEffect(() => {
    const local = safeRead(storageKey, tenantID);
    setTabs(local.tabs);setMountedEditors([]);setEditorDirty({});setRecent(local.recent);setFavorites(local.favorites);setSelectedKey("");
    if (local.query) {
      setBaseQuery({ ...defaultQuery(allowedTypes), ...local.query, query: urlParams.get("q") ?? "" });
      const group = local.query.filters;
      setFilterLogic(group?.operator === "OR" ? "OR" : "AND");
      setFilterRows((group?.children ?? []).map(filterDraftFromNode).filter((node):node is FilterDraftNode=>node!==null));
    } else {
      setBaseQuery(defaultQuery(allowedTypes));setFilterRows([]);setFilterLogic("AND");
    }
  }, [storageKey, tenantID]);

  useEffect(() => {
    if (!selectedKey || !tabs.some((tab) => tab.key === selectedKey)) return;
    setMountedEditors((current) => current.includes(selectedKey) ? current : [...current, selectedKey]);
  }, [selectedKey, tabs]);

  useEffect(() => {
    if (pendingClose) closeDialogFirst.current?.focus();
  }, [pendingClose]);

  useEffect(() => {
    const queryFromURL = urlParams.get("q") ?? "";
    if (queryFromURL) setBaseQuery((current) => current.query === queryFromURL ? current : { ...current, query: queryFromURL, sort: "rank", cursor: undefined });
  }, [urlParams]);

  useEffect(() => {
    try { localStorage.setItem(storageKey, JSON.stringify({ tabs, recent, favorites, query: { view: baseQuery.view, sort: baseQuery.sort, group_by: baseQuery.group_by, types: baseQuery.types, columns: baseQuery.columns, column_order: baseQuery.column_order, column_widths: baseQuery.column_widths, limit: baseQuery.limit, filters: requestQuery.filters } } satisfies LocalState)); } catch { /* Browser storage can be disabled; server views remain available. */ }
  }, [storageKey, tabs, recent, favorites, baseQuery.view, baseQuery.sort, baseQuery.group_by, baseQuery.types, baseQuery.columns, baseQuery.column_order, baseQuery.column_widths, baseQuery.limit, requestQuery.filters]);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError("");
    if (!fieldsReady) return () => controller.abort();
    const timer = window.setTimeout(() => {
      void queryWork(requestQuery, controller.signal)
        .then((page) => { if (!controller.signal.aborted) {setItems(page.items);setNextCursor(page.next_cursor??"");} })
        .catch((cause) => { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : "Nie udało się pobrać kolejki."); })
        .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    }, 220);
    return () => { window.clearTimeout(timer); controller.abort(); };
  }, [requestQuery, refreshKey, fieldsReady]);

  useEffect(() => {
    if (!selectedKey) { setDetail(null); setDetailError(""); return; }
    const tab = [...tabs, ...favorites, ...recent].find((item) => item.key === selectedKey);
    if (!tab) { setDetail(null); return; }
    const controller = new AbortController();
    setDetailLoading(true);setDetailError("");
    void getWorkItem(tab.object_type, tab.object_id, controller.signal)
      .then((item) => { if (!controller.signal.aborted) setDetail(item); })
      .catch((cause) => { if (!controller.signal.aborted) { setDetail(null);setDetailError(cause instanceof Error ? cause.message : "Rekord nie jest już dostępny."); } })
      .finally(() => { if (!controller.signal.aborted) setDetailLoading(false); });
    return () => controller.abort();
  }, [selectedKey, tabs, favorites, recent, refreshKey]);

  useEffect(() => {
    if (!exportStatus || !["QUEUED", "RUNNING", "RETRYING"].includes(exportStatus.status)) return;
    if (exportError) return;
    if (exportPollAttempts.current >= 12) {
      setExportError("Automatyczne sprawdzanie zatrzymano. Możesz ręcznie sprawdzić status eksportu.");
      return;
    }
    const attempt = exportPollAttempts.current++;
    const delay = Math.min(15000, 1800 * 2 ** Math.min(attempt, 4));
    const timer = window.setTimeout(() => { void checkExportStatus(); }, delay);
    return () => window.clearTimeout(timer);
  }, [exportStatus, exportError]);

  useEffect(() => {
    function onShortcut(event:KeyboardEvent) {
      if (event.altKey||event.ctrlKey||event.metaKey) return;
      const target=event.target as HTMLElement|null;
      if (target?.matches("input,textarea,select,[contenteditable=true]")) return;
      if (event.key==="/") {event.preventDefault();document.getElementById("workspace-search")?.focus();return;}
      if (event.key==="j"||event.key==="k") {
        if (!items.length) return;
        event.preventDefault();
        const current=items.findIndex((item)=>referenceKey(item)===selectedKey);
        const next=event.key==="j"?Math.min(items.length-1,current+1):Math.max(0,current<0?0:current-1);
        const item=items[next];if(item)openItem(item);
      }
    }
    window.addEventListener("keydown",onShortcut);return()=>window.removeEventListener("keydown",onShortcut);
  }, [items,selectedKey]);

  function openItem(item: WorkItem | SavedReference) {
    const ref = { tenant_id: item.tenant_id, object_type: item.object_type, object_id: item.object_id };
    const key = referenceKey(ref);
    const saved: SavedReference = { ...ref, key, title: item.title, number: item.number, record_path: recordPath(ref) };
    setTabs((current) => [saved, ...current.filter((tab) => tab.key !== key)].slice(0, 8));
    setRecent((current) => [saved, ...current.filter((tab) => tab.key !== key)].slice(0, 10));
    setSelectedKey(key);
  }

  function changeBase<K extends keyof WorkQuery>(key: K, value: WorkQuery[K]) {
    setBaseQuery((current) => ({ ...current, [key]: value, cursor: undefined }));
    setNextCursor("");
    setSelectedRows([]);setResults([]);
    setSelectionError("");
  }

  function applySavedView(id: string) {
    setChosenView(id);
    const view = views.find((candidate) => candidate.id === id);
    if (!view) { setBaseQuery(defaultQuery(allowedTypes));setFilterRows([]);return; }
    const definition = { ...defaultQuery(allowedTypes), ...view.definition, cursor: undefined };
    setBaseQuery(definition);
    const group = definition.filters;
    setFilterLogic(group?.operator === "OR" ? "OR" : "AND");
    setFilterRows((group?.children ?? []).map(filterDraftFromNode).filter((node):node is FilterDraftNode=>node!==null));
  }

  function addFilter(path?:number[]) { setFilterRows((current)=>appendFilter(current,path,{kind:"condition",field:"number",compare:"contains",value:""})); }
  function addFilterGroup(path?:number[]) { setFilterRows((current)=>appendFilter(current,path,{kind:"group",operator:"AND",children:[{kind:"condition",field:"number",compare:"contains",value:""}]})); }
  function updateFilter(path:number[],patch:Partial<FilterConditionDraft>) { setFilterRows((current)=>replaceFilter(current,path,(node)=>node.kind==="condition"?{...node,...patch}:node)); }
  function updateFilterGroup(path:number[],operator:"AND"|"OR") { setFilterRows((current)=>replaceFilter(current,path,(node)=>node.kind==="group"?{...node,operator}:node)); }
  function removeFilter(path:number[]) { setFilterRows((current)=>removeFilterAt(current,path)); }

  async function saveView() {
    if (!viewName.trim()) return;
    try {
      const saved = await saveWorkView({ name: viewName.trim(), shared: shareView, definition: requestQuery });
      setViews((current) => [saved, ...current.filter((view) => view.id !== saved.id)]);
      setChosenView(saved.id);setViewName("");setViewsError("");
    } catch (cause) { setViewsError(cause instanceof Error ? cause.message : "Nie udało się zapisać widoku."); }
  }

  async function removeView() {
    const view = views.find((candidate) => candidate.id === chosenView);
    if (!view) return;
    try { await deleteWorkView(view.id, view.version);setViews((current) => current.filter((candidate) => candidate.id !== view.id));setChosenView(""); }
    catch (cause) { setViewsError(cause instanceof Error ? cause.message : "Nie udało się usunąć widoku."); }
  }

  async function applyAction(targets: WorkItem[], action: "assign" | "transition", state = bulkState) {
    if (!targets.length || actionBusy) return;
    setActionBusy(true);setResults([]);
    try {
      const response = await runWorkAction({
        action,
        items: targets.map((item) => ({ tenant_id: item.tenant_id, object_type: item.object_type, object_id: item.object_id, version: item.version, assignment_group_id: item.assignment_group_id })),
        ...(action === "assign" ? { assigned_to: userID,reason } : { state, reason,hold_reason:holdReason, ...(state === "RESOLVED" ? { resolution_code: resolutionCode, resolution_description: resolutionDescription } : {}) }),
      });
      setResults(response.results);setSelectedRows([]);setRefreshKey((value) => value + 1);
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Nie udało się wykonać akcji."); }
    finally { setActionBusy(false); }
  }

  function toggleRow(item: WorkItem) { const key = referenceKey(item);setSelectedRows((current) => { if(current.includes(key))return current.filter((entry)=>entry!==key);if(current.length>=50){setSelectionError("Możesz wykonać operację zbiorczą maksymalnie dla 50 rekordów.");return current;}setSelectionError("");return [...current,key]; }); }
  function selectVisibleRows(checked:boolean) { if(!checked){setSelectedRows([]);setSelectionError("");return;}const visible=items.slice(0,50).map(referenceKey);setSelectedRows(visible);setSelectionError(items.length>50?"Zaznaczono pierwsze 50 rekordów. Operacje zbiorcze obsługują maksymalnie 50.":""); }
  function toggleFavorite(ref: SavedReference) { setFavorites((current) => current.some((item) => item.key === ref.key) ? current.filter((item) => item.key !== ref.key) : [ref, ...current].slice(0, 20)); }
  function removeTab(key: string) {
    setTabs((current) => current.filter((tab) => tab.key !== key));
    setMountedEditors((current) => current.filter((entry) => entry !== key));
    setEditorDirty((current) => { const next = { ...current }; delete next[key]; return next; });
    if (selectedKey === key) setSelectedKey(tabs.find((tab) => tab.key !== key)?.key ?? "");
  }
  function closeTab(key: string) {
    if (editorDirty[key]) { setPendingClose(key); return; }
    removeTab(key);
  }

  async function exportQueue() {
    if (exportBusy) return;setExportBusy(true);setExportError("");
    exportPollAttempts.current = 0;
    try { const status = await requestWorkExport(requestQuery, crypto.randomUUID());setExportStatus(status); }
    catch (cause) { setExportError(cause instanceof Error ? cause.message : "Nie udało się przygotować eksportu."); }
    finally { setExportBusy(false); }
  }

  async function checkExportStatus() {
    if (!exportStatus || exportCheckBusy) return;
    setExportCheckBusy(true);setExportError("");
    try { exportPollAttempts.current = 0;setExportStatus(await getWorkExport(exportStatus.job_id)); }
    catch (cause) { setExportError(cause instanceof Error ? cause.message : "Nie udało się sprawdzić eksportu."); }
    finally { setExportCheckBusy(false); }
  }

  async function loadMore() {
    if (!nextCursor||moreBusy) return;
    setMoreBusy(true);
    try { const page=await queryWork({...requestQuery,cursor:nextCursor});setItems((current)=>[...current,...page.items]);setNextCursor(page.next_cursor??""); }
    catch(cause) {setError(cause instanceof Error?cause.message:"Nie udało się pobrać kolejnej strony.");}
    finally {setMoreBusy(false);}
  }

  const selectedItems = items.filter((item) => selectedRows.includes(referenceKey(item)));
  const visibleColumns = (baseQuery.column_order?.filter((column) => baseQuery.columns?.includes(column)) ?? baseQuery.columns ?? initialColumns).filter((column) => baseQuery.columns?.includes(column));
  const currentRef = [...tabs, ...favorites, ...recent].find((item) => item.key === selectedKey);

  return (
    <section className="page workspace-page">
      <UnsavedGuard dirty={Object.values(editorDirty).some(Boolean)} />
      <div className="page-heading">
        <div><p className="eyebrow">Obsługa operacyjna</p><h1>Workspace operatora</h1><p className="muted">Wspólna, filtrowana kolejka incydentów i zadań realizacji.</p></div>
        <button className="button button--secondary" type="button" onClick={() => setRefreshKey((value) => value + 1)} aria-label="Odśwież kolejkę">Odśwież</button>
      </div>

      <div className="workspace-layout">
        <div className="workspace-queue">
          <section className="panel workspace-controls" aria-label="Filtry kolejki">
            <div className="form-grid workspace-filter-grid">
              <label>Szukaj<input id="workspace-search" value={baseQuery.query ?? ""} maxLength={200} onChange={(event) => changeBase("query", event.target.value)} placeholder="Numer lub tytuł" /></label>
              <label>Widok<select value={baseQuery.view} onChange={(event) => changeBase("view", event.target.value as WorkQuery["view"])}><option value="mine">Moja praca</option><option value="group">Moich grup</option><option value="unassigned">Nieprzypisane</option><option value="watched">Obserwowane</option><option value="all">Wszystkie dostępne</option></select></label>
              <label>Sortowanie<select value={baseQuery.sort} onChange={(event) => changeBase("sort", event.target.value as WorkQuery["sort"])}><option value="updated_desc">Ostatnio zmienione</option><option value="created_desc">Najnowsze</option><option value="priority_asc">Priorytet</option><option value="number_asc">Numer</option>{baseQuery.query ? <option value="rank">Trafność</option> : null}</select></label>
              <label>Grupuj według<select value={baseQuery.group_by ?? ""} onChange={(event) => changeBase("group_by", event.target.value)}><option value="">Bez grupowania</option><option value="object_type">Typ pracy</option><option value="state">Stan</option><option value="state_category">Kategoria stanu</option><option value="priority">Priorytet</option><option value="assignment_group_id">Grupa przypisania</option></select></label>
            </div>
            <div className="workspace-type-filters" aria-label="Typy pracy">
              {canIncidents ? <label><input type="checkbox" checked={baseQuery.types?.includes("incident") ?? false} onChange={(event) => changeBase("types", event.target.checked ? [...(baseQuery.types ?? []), "incident"] : (baseQuery.types ?? []).filter((type) => type !== "incident"))} /> Incydenty</label> : null}
              {canTasks ? <label><input type="checkbox" checked={baseQuery.types?.includes("fulfillment_task") ?? false} onChange={(event) => changeBase("types", event.target.checked ? [...(baseQuery.types ?? []), "fulfillment_task"] : (baseQuery.types ?? []).filter((type) => type !== "fulfillment_task"))} /> SCTASK</label> : null}
            </div>

            <details className="workspace-filter-builder">
              <summary>Konstruktor filtrów ({filterDraftCount(filterRows)})</summary>
              <div className="workspace-filter-actions"><label>Warunki<select value={filterLogic} onChange={(event) => setFilterLogic(event.target.value as "AND" | "OR")}><option value="AND">Wszystkie (AND)</option><option value="OR">Dowolny (OR)</option></select></label><button className="button button--secondary" type="button" onClick={()=>addFilter()}>Dodaj warunek</button><button className="button button--secondary" type="button" onClick={()=>addFilterGroup()}>Dodaj grupę z nawiasem</button></div>
              {filterRows.map((node,index) => <FilterDraftEditor key={index} node={node} path={[index]} fields={fields} onChange={updateFilter} onGroupChange={updateFilterGroup} onAddCondition={addFilter} onAddGroup={addFilterGroup} onRemove={removeFilter} />)}
            </details>

            <details className="workspace-columns">
              <summary>Kolumny i rozmiar strony</summary>
              <div className="workspace-column-options">{fields.filter((field)=>!field.filter_only).map((field) => {
                const columns=baseQuery.columns ?? initialColumns;
                const order=baseQuery.column_order ?? columns;
                const index=order.indexOf(field.key);
                return <div className="workspace-column-option" key={field.key}><label><input type="checkbox" checked={columns.includes(field.key)} onChange={(event) => { changeBase("columns",event.target.checked ? [...columns,field.key] : columns.filter((key)=>key!==field.key));if(event.target.checked&&!order.includes(field.key))changeBase("column_order",[...order,field.key]); }} /> {field.label}</label><label className="workspace-width-label">Szerokość <input type="number" min={80} max={800} disabled={!columns.includes(field.key)} value={baseQuery.column_widths?.[field.key] ?? 160} onChange={(event)=>changeBase("column_widths",{...baseQuery.column_widths,[field.key]:Number(event.target.value)})} /></label><div><button type="button" className="button button--small button--secondary" disabled={index<=0} aria-label={`Przesuń kolumnę ${field.label} w lewo`} onClick={()=>{const next=[...order];[next[index-1],next[index]]=[next[index]!,next[index-1]!];changeBase("column_order",next);}}>←</button><button type="button" className="button button--small button--secondary" disabled={index<0||index>=order.length-1} aria-label={`Przesuń kolumnę ${field.label} w prawo`} onClick={()=>{const next=[...order];[next[index+1],next[index]]=[next[index]!,next[index+1]!];changeBase("column_order",next);}}>→</button></div></div>;
              })}</div>
              <label>Wierszy na stronę<select value={baseQuery.limit} onChange={(event) => changeBase("limit",Number(event.target.value))}><option value={25}>25</option><option value={50}>50</option><option value={100}>100</option></select></label>
            </details>

            <div className="workspace-saved-view-controls">
              <label>Widok zapisany<select value={chosenView} onChange={(event) => applySavedView(event.target.value)}><option value="">Bieżące ustawienia</option>{views.map((view) => <option key={view.id} value={view.id}>{view.name}{view.shared ? " · wspólny" : ""}</option>)}</select></label>
              <label>Nazwa widoku<input value={viewName} maxLength={80} onChange={(event) => setViewName(event.target.value)} /></label>
              <label className="workspace-share-toggle"><input type="checkbox" checked={shareView} disabled={!canShare} onChange={(event) => setShareView(event.target.checked)} /> Udostępnij zespołowi</label>
              <button className="button button--secondary" type="button" disabled={!viewName.trim()} onClick={() => void saveView()}>Zapisz widok</button>
              {chosenView && views.some((view) => view.id===chosenView && (!view.shared || canShare)) ? <button className="button button--secondary" type="button" onClick={() => void removeView()}>Usuń widok</button> : null}
            </div>
            {viewsError ? <p role="alert" className="error-text">{viewsError}</p> : null}
          </section>

          {selectedRows.length ? <section className="panel workspace-bulk" aria-label="Operacje zbiorcze">
            <strong>{selectedRows.length} zaznaczonych</strong>{selectionError?<p role="status" className="muted">{selectionError}</p>:null}
            <label>Akcja<select value={bulkAction} onChange={(event) => setBulkAction(event.target.value as "assign"|"transition")}><option value="assign">Przypisz do mnie</option><option value="transition">Zmień stan</option></select></label>
            {bulkAction === "transition" ? <label>Docelowy stan<select value={bulkState} onChange={(event) => setBulkState(event.target.value)}>{states.map((state) => <option key={state} value={state}>{stateLabel(state)}</option>)}</select></label> : null}
            {bulkAction === "transition" ? <label>Uzasadnienie<input value={reason} maxLength={10000} onChange={(event) => setReason(event.target.value)} /></label> : null}
            {bulkAction === "transition" && bulkState === "RESOLVED" ? <><label>Kod rozwiązania<select value={resolutionCode} onChange={(event) => setResolutionCode(event.target.value)}>{incidentOptions.data?.resolution_codes.filter(o=>!o.retired).map(o=><option key={o.value} value={o.value}>{o.label}</option>)}</select></label><label>Opis rozwiązania<input value={resolutionDescription} maxLength={20000} onChange={(event) => setResolutionDescription(event.target.value)} /></label></> : null}
            <button className="button" type="button" disabled={actionBusy || !selectedItems.length || (bulkAction==="assign"&&!reason.trim())} onClick={() => void applyAction(selectedItems,bulkAction)}>{actionBusy ? "Zapisuję…" : "Zastosuj dla zaznaczonych"}</button>
          </section> : null}

          <section className="panel"><label>{module08Text.override}<input value={reason} maxLength={2000} onChange={e=>setReason(e.target.value)}/></label><label>{module08Text.holdReason}<select value={holdReason} onChange={e=>setHoldReason(e.target.value)}><option value="">—</option>{incidentOptions.data?.hold_reasons?.filter(o=>!o.retired).map(o=><option key={o.value} value={o.value}>{o.label}</option>)}</select></label>{incidentOptions.error&&<p role="alert">{incidentOptions.error}</p>}</section>
          {Object.values(inlineStates).includes("RESOLVED") ? <section className="panel workspace-inline-resolution"><h2>Dane wymagane do rozwiązania</h2><label>Kod rozwiązania<select value={resolutionCode} onChange={(event)=>setResolutionCode(event.target.value)}>{incidentOptions.data?.resolution_codes.filter(o=>!o.retired).map(o=><option key={o.value} value={o.value}>{o.label}</option>)}</select></label><label>Opis rozwiązania<input value={resolutionDescription} maxLength={20000} onChange={(event)=>setResolutionDescription(event.target.value)} /></label><label>Uzasadnienie<input value={reason} maxLength={10000} onChange={(event)=>setReason(event.target.value)} /></label></section> : null}

          <section className="panel workspace-list" aria-label="Moja praca">
            <div className="workspace-list-heading"><h2>Moja praca</h2><span>{items.length}{loading ? " · ładowanie" : ""}</span><button className="button button--secondary" type="button" disabled={loading || exportBusy} onClick={() => void exportQueue()}>{exportBusy ? "Przygotowuję…" : "Eksportuj CSV"}</button></div>
            {error ? <PageState title="Nie udało się pobrać kolejki" detail={error} tone="error" /> : null}
            {loading && !items.length ? <PageState title="Ładowanie kolejki…" /> : null}
            {!loading && !error && !items.length ? <PageState title="Brak dostępnych rekordów" detail="Zmień filtry lub wybierz inny widok." /> : null}
            {items.length ? <><p className="muted workspace-shortcut-hint">Skróty: / wyszukuje, j/k przechodzi między rekordami.</p><div className="table-scroll workspace-table-scroll"><table className="workspace-table"><thead><tr><th><input type="checkbox" aria-label="Zaznacz wszystkie widoczne rekordy" checked={items.length>0&&items.every((item)=>selectedRows.includes(referenceKey(item)))} onChange={(event)=>selectVisibleRows(event.target.checked)} /></th>{visibleColumns.map((column)=><th key={column}>{fields.find((field)=>field.key===column)?.label??column}</th>)}<th>Akcje</th></tr></thead><tbody>
              {displayGroups(items,baseQuery.group_by).flatMap((group)=>([{kind:"header" as const,key:`group:${group.label}`,label:group.label},...group.items.map((item)=>({kind:"item" as const,key:referenceKey(item),item}))])).map((entry)=><Fragment key={entry.key}>{entry.kind==="header"&&baseQuery.group_by?<tr className="workspace-group-row"><th colSpan={visibleColumns.length+2}>{entry.label}</th></tr>:null}{entry.kind==="item"?<tr className={selectedKey===referenceKey(entry.item)?"is-selected":""}>
                <td><input type="checkbox" aria-label={`Zaznacz ${entry.item.number}`} checked={selectedRows.includes(referenceKey(entry.item))} onChange={()=>toggleRow(entry.item)} /></td>
                {visibleColumns.map((column)=><td key={column} style={{width:baseQuery.column_widths?.[column]}}>{renderColumn(entry.item,column,()=>openItem(entry.item))}</td>)}
                <td className="workspace-row-actions"><button className="button button--small button--secondary" type="button" onClick={()=>openItem(entry.item)}>Podgląd</button>{entry.item.capabilities.includes("assign")?<button className="button button--small button--secondary" type="button" disabled={actionBusy||!reason.trim()} onClick={()=>void applyAction([entry.item],"assign")}>Przypisz mi</button>:null}<select aria-label={`Zmień stan ${entry.item.number}`} value={inlineStates[referenceKey(entry.item)]??""} onChange={(event)=>setInlineStates((current)=>({...current,[referenceKey(entry.item)]:event.target.value}))}><option value="">Stan…</option>{entry.item.capabilities.filter((cap)=>cap.startsWith("transition:")).map((cap)=><option key={cap} value={cap.slice("transition:".length)}>{stateLabel(cap.slice("transition:".length))}</option>)}</select><button className="button button--small button--secondary" type="button" disabled={!inlineStates[referenceKey(entry.item)]||actionBusy} onClick={()=>void applyAction([entry.item],"transition",inlineStates[referenceKey(entry.item)]!)}>Zastosuj</button></td>
              </tr>:null}</Fragment>)}
            </tbody></table></div>{nextCursor?<button className="button button--secondary mt-3" type="button" disabled={moreBusy} onClick={()=>void loadMore()}>{moreBusy?"Ładowanie…":"Pokaż więcej"}</button>:null}</> : null}
            {exportError ? <p role="alert" className="error-text">{exportError}</p> : null}
            {exportStatus ? <div className="workspace-export-status" role="status"><p>Eksport: {exportStatus.status}{exportStatus.download_path ? <> · <a href={exportStatus.download_path}>Pobierz CSV</a></> : exportStatus.error ? ` · ${exportStatus.error}` : "…"}</p>{["QUEUED", "RUNNING", "RETRYING"].includes(exportStatus.status) ? <button className="button button--small button--secondary" type="button" disabled={exportCheckBusy} onClick={() => void checkExportStatus()}>{exportCheckBusy ? "Sprawdzam…" : "Sprawdź status"}</button> : null}</div> : null}
            <button className="button button--secondary mt-3" type="button" disabled={loading||!items.length} onClick={()=>setRefreshKey((value)=>value+1)}>Odśwież wyniki</button>
          </section>

          <ActionResults results={results} />
        </div>

        <aside className="panel workspace-side" aria-label="Zakładki i podgląd pracy">
          <h2>Otwarte rekordy</h2>
          {tabs.length ? <div className="workspace-tabs" role="tablist" aria-label="Otwarte rekordy">{tabs.map((tab)=><div className="workspace-tab" key={tab.key}><button type="button" role="tab" aria-selected={selectedKey===tab.key} onClick={()=>setSelectedKey(tab.key)} title={`${tab.number} ${tab.title}`}>{tab.number}</button><button type="button" className="workspace-tab-close" aria-label={`Zamknij ${tab.number}`} onClick={()=>closeTab(tab.key)}>×</button></div>)}</div> : <p className="muted">Otwórz rekord z kolejki, aby dodać zakładkę.</p>}
          {currentRef ? <div className="workspace-preview-actions"><button className="button button--secondary" type="button" onClick={()=>toggleFavorite(currentRef)}>{favorites.some((item)=>item.key===currentRef.key)?"Usuń z ulubionych":"Dodaj do ulubionych"}</button><Link className="button button--secondary" to={recordPath(currentRef)}>Otwórz pełny formularz</Link></div> : null}
          {detailLoading ? <PageState title="Ładowanie podglądu…" /> : null}
          {detailError ? <PageState title="Rekord niedostępny" detail={detailError} tone="error" /> : null}
          {detail ? <div className="workspace-preview"><p className="eyebrow">{detail.object_type=== "incident" ? "Incydent" : "Zadanie SCTASK"}</p><h3>{detail.number} · {detail.title}</h3><p>{stateLabel(detail.state)}{detail.priority ? ` · P${detail.priority}` : ""}</p><dl><dt>Przypisany</dt><dd>{detail.assigned_name||"Nieprzypisany"}</dd><dt>Grupa</dt><dd>{detail.group_name||"Brak grupy"}</dd><dt>Utworzono</dt><dd>{formatDate(detail.created_at)}</dd><dt>Ostatnia zmiana</dt><dd>{formatDate(detail.updated_at)}</dd><dt>Wersja</dt><dd>{detail.version}</dd></dl><p className="muted">Pełny formularz, komunikacja, pliki, historia i reguły procesu są osadzone poniżej.</p></div> : null}
          <h3>Ulubione</h3>{favorites.length?<ul className="workspace-link-list">{favorites.map((item)=><li key={item.key}><button type="button" onClick={()=>openItem(item)}>{item.number} · {item.title}</button></li>)}</ul>:<p className="muted">Brak ulubionych rekordów.</p>}
          <h3>Ostatnio odwiedzone</h3>{recent.length?<ul className="workspace-link-list">{recent.slice(0,8).map((item)=><li key={item.key}><button type="button" onClick={()=>openItem(item)}>{item.number} · {item.title}</button></li>)}</ul>:<p className="muted">Brak ostatnich rekordów.</p>}
        </aside>
      </div>
      {tabs.filter((tab) => mountedEditors.includes(tab.key)).map((tab) => (
        <div
          key={tab.key}
          className="workspace-editor-mount"
          hidden={selectedKey !== tab.key}
          aria-label={`Formularz ${tab.number}`}
        >
          <WorkRecordEditor reference={tab} onDirtyChange={reportEditorDirty} />
        </div>
      ))}
      {pendingClose ? (
        <div className="dialog-backdrop">
          <section className="panel confirm-dialog" role="alertdialog" aria-modal="true" aria-labelledby="workspace-close-title" aria-describedby="workspace-close-description" onKeyDown={(event) => {
            if (event.key === "Escape") setPendingClose("");
            if (event.key === "Tab") {
              const buttons = event.currentTarget.querySelectorAll("button");
              const first = buttons[0], last = buttons[buttons.length - 1];
              if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
              else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
            }
          }}>
            <h2 id="workspace-close-title">Niezapisany szkic</h2>
            <p id="workspace-close-description">Ta zakładka zawiera niezapisane zmiany. Możesz wrócić do formularza albo odrzucić szkic i zamknąć rekord.</p>
            <div className="action-row">
              <button ref={closeDialogFirst} className="button button--primary" type="button" onClick={() => { setSelectedKey(pendingClose); setPendingClose(""); }}>Wróć do formularza</button>
              <button className="button button--secondary" type="button" onClick={() => { const key = pendingClose; setPendingClose(""); removeTab(key); }}>Odrzuć szkic i zamknij</button>
            </div>
          </section>
        </div>
      ) : null}
    </section>
  );
}

function filterDraftFromNode(node:FilterNode):FilterDraftNode|null {
  if(node.children?.length&&node.operator) {
    const children=node.children.map(filterDraftFromNode).filter((child):child is FilterDraftNode=>child!==null);
    return {kind:"group",operator:node.operator,children};
  }
  if(node.field) return {kind:"condition",field:node.field,compare:node.compare??"eq",value:filterInputValue(node.value)};
  return null;
}

function filterDraftToNode(node:FilterDraftNode,fields:FieldDefinition[]):FilterNode|null {
  if(node.kind==="group") {
    const children=node.children.map((child)=>filterDraftToNode(child,fields)).filter((child):child is FilterNode=>child!==null);
    return children.length?{operator:node.operator,children}:null;
  }
  if(!node.field||!node.compare||node.compare!=="is_empty"&&node.value==="")return null;
  return {field:node.field,compare:node.compare,value:filterValue(node,fields)};
}

function filterDraftCount(nodes:FilterDraftNode[]):number {
  return nodes.reduce((count,node)=>count+(node.kind==="group"?filterDraftCount(node.children):node.compare==="is_empty"||node.value!==""?1:0),0);
}

function appendFilter(nodes:FilterDraftNode[],path:number[]|undefined,value:FilterDraftNode):FilterDraftNode[] {
  if(!path?.length)return [...nodes,value];
  const [index,...rest]=path;
  return nodes.map((node,current)=>{
    if(current!==index||node.kind!=="group")return node;
    if(rest.length===0)return {...node,children:[...node.children,value]};
    return {...node,children:appendFilter(node.children,rest,value)};
  });
}

function replaceFilter(nodes:FilterDraftNode[],path:number[],update:(node:FilterDraftNode)=>FilterDraftNode):FilterDraftNode[] {
  const [index,...rest]=path;
  return nodes.map((node,current)=>{
    if(current!==index)return node;
    if(rest.length===0)return update(node);
    if(node.kind!=="group")return node;
    return {...node,children:replaceFilter(node.children,rest,update)};
  });
}

function removeFilterAt(nodes:FilterDraftNode[],path:number[]):FilterDraftNode[] {
  const [index,...rest]=path;
  if(rest.length===0)return nodes.filter((_,current)=>current!==index);
  return nodes.map((node,current)=>current===index&&node.kind==="group"?{...node,children:removeFilterAt(node.children,rest)}:node);
}

function filterValue(row: FilterConditionDraft, fields: FieldDefinition[]): FilterNode["value"] {
  const type=fields.find((field)=>field.key===row.field)?.type;
  if(row.compare==="is_empty") return undefined;
  if(row.compare==="within_last_days") return Number(row.value);
  if(row.compare==="in") return row.value.split(",").map((value)=>value.trim()).filter(Boolean);
  if(type==="number"||type==="integer"||type==="decimal") return Number(row.value);
  if(type==="boolean") return row.value==="true";
  if(type==="date_only") return row.value;
  if(type==="date"||type==="datetime") {const date=new Date(row.value);return Number.isNaN(date.valueOf())?row.value:date.toISOString();}
  return row.value;
}

function filterInputValue(value:FilterNode["value"]):string {
  if(Array.isArray(value)) return value.join(", ");
  if(value===undefined||value===null) return "";
  if(typeof value==="boolean") return value?"true":"false";
  if(typeof value==="number") return String(value);
  if(/T/.test(value)) {const date=new Date(value);if(!Number.isNaN(date.valueOf()))return new Date(date.getTime()-date.getTimezoneOffset()*60000).toISOString().slice(0,16);}
  return value;
}

function FilterDraftEditor({node,path,fields,onChange,onGroupChange,onAddCondition,onAddGroup,onRemove}:{node:FilterDraftNode;path:number[];fields:FieldDefinition[];onChange:(path:number[],patch:Partial<FilterConditionDraft>)=>void;onGroupChange:(path:number[],operator:"AND"|"OR")=>void;onAddCondition:(path?:number[])=>void;onAddGroup:(path?:number[])=>void;onRemove:(path:number[])=>void}) {
  if(node.kind==="condition") return <FilterEditor row={node} fields={fields} onChange={(patch)=>onChange(path,patch)} onRemove={()=>onRemove(path)} />;
  return <fieldset className="workspace-filter-group"><legend>Grupa z nawiasem</legend><label>Warunki<select value={node.operator} onChange={(event)=>onGroupChange(path,event.target.value as "AND"|"OR")}><option value="AND">Wszystkie (AND)</option><option value="OR">Dowolny (OR)</option></select></label>{node.children.map((child,index)=><FilterDraftEditor key={index} node={child} path={[...path,index]} fields={fields} onChange={onChange} onGroupChange={onGroupChange} onAddCondition={onAddCondition} onAddGroup={onAddGroup} onRemove={onRemove} />)}<div className="workspace-filter-actions"><button className="button button--small button--secondary" type="button" onClick={()=>onAddCondition(path)}>Dodaj warunek</button><button className="button button--small button--secondary" type="button" onClick={()=>onAddGroup(path)}>Dodaj podgrupe</button><button className="button button--small button--secondary" type="button" onClick={()=>onRemove(path)}>Usun grupe</button></div></fieldset>;
}

function FilterEditor({ row,fields,onChange,onRemove }: { row:FilterConditionDraft;fields:FieldDefinition[];onChange:(patch:Partial<FilterConditionDraft>)=>void;onRemove:()=>void }) {
  const filterFields=fields.filter((item)=>item.operators.length>0);
  const field=filterFields.find((item)=>item.key===row.field);
  const operator=row.compare;
  return <div className="workspace-filter-row">
    <label>Pole<select value={row.field} onChange={(event)=>{const next=filterFields.find((item)=>item.key===event.target.value);onChange({field:event.target.value,compare:next?.operators[0]??"eq",value:""});}}>{filterFields.map((item)=><option key={item.key} value={item.key}>{item.label}</option>)}</select></label>
    <label>Warunek<select value={operator} onChange={(event)=>onChange({compare:event.target.value,value:""})}>{(field?.operators??[]).map((value)=><option key={value} value={value}>{({eq:"rowna sie",neq:"rozne od",contains:"zawiera",starts_with:"zaczyna sie od",gt:"wieksze niz",gte:"co najmniej",lt:"mniejsze niz",lte:"nie wieksze niz",before:"przed",after:"po",within_last_days:"ostatnie dni",is_empty:"puste",in:"jedna z"} as Record<string,string>)[value]??value}</option>)}</select></label>
    {renderFilterValue(field,row,onChange,operator)}
    <button className="button button--small button--secondary" type="button" aria-label={`Usun warunek ${row.field}`} onClick={onRemove}>Usun</button>
  </div>;
}

function renderFilterValue(field:FieldDefinition|undefined,row:FilterConditionDraft,onChange:(patch:Partial<FilterConditionDraft>)=>void,operator:string) {
  if(operator==="is_empty") return null;
  if(field?.type==="reference"&&(field.reference_type==="group"||field.reference_type==="user")) return <RemoteCombobox kind={field.reference_type==="group"?"groups":"users"} endpoint={`/api/v1/workspace/references/${field.reference_type==="group"?"groups":"users"}`} collection="items" cursor="after_id" label={field.label} value={row.value?{id:row.value,label:row.value} as Choice:null} onChange={(choice)=>onChange({value:choice?.id??""})} />;
  if(field?.type==="boolean") return <label>Wartość<select value={row.value} onChange={(event)=>onChange({value:event.target.value})}><option value="">Wybierz…</option><option value="true">Tak</option><option value="false">Nie</option></select></label>;
  if(field?.type==="enum"&&field.options?.length&&operator!=="in") return <label>Wartość<select value={row.value} onChange={(event)=>onChange({value:event.target.value})}><option value="">Wybierz…</option>{field.options.map((option)=><option key={option.value} value={option.value}>{option.label}</option>)}</select></label>;
  const type=field?.type==="number"||field?.type==="integer"||field?.type==="decimal"||operator==="within_last_days"?"number":field?.type==="date"||field?.type==="datetime"?"datetime-local":field?.type==="date_only"?"date":"text";
  return <label>Wartość<input type={type} step={field?.type==="decimal"?"any":undefined} min={operator==="within_last_days"?1:undefined} max={operator==="within_last_days"?3650:undefined} maxLength={200} value={row.value} onChange={(event)=>onChange({value:event.target.value})} /></label>;
}

function renderColumn(item:WorkItem,column:string,onOpen:()=>void) {
  if(column.startsWith("cf.")) return formatCustomField(item.custom_fields?.[column.slice(3)]);
  switch(column) {
    case "number":return <button className="workspace-number" type="button" onClick={onOpen}>{item.number}</button>;
    case "object_type":return item.object_type==="incident"?"INC":"SCTASK";
    case "title":return <button className="workspace-title" type="button" onClick={onOpen}>{item.title}</button>;
    case "state":return stateLabel(item.state);
    case "state_category":return item.state_category;
    case "priority":return item.priority?`P${item.priority}`:"—";
    case "assigned_to":return item.assigned_name||"Nieprzypisany";
    case "assignment_group_id":return item.group_name||"Bez grupy";
    case "created_at":return formatDate(item.created_at);
    case "updated_at":return formatDate(item.updated_at);
    case "category":return item.category||"—";
    case "watched":return item.watched?"Tak":"Nie";
    default:return "—";
  }
}

function formatCustomField(value:string|number|boolean|string[]|null|undefined):string {
  if(value===undefined||value===null||value==="")return "—";
  if(Array.isArray(value))return value.join(", ");
  if(typeof value==="boolean")return value?"Tak":"Nie";
  return String(value);
}

function formatDate(value:string) {
  const date=new Date(value);return Number.isNaN(date.valueOf())?"—":new Intl.DateTimeFormat(undefined,{dateStyle:"medium",timeStyle:"short"}).format(date);
}

function displayGroups(items:WorkItem[],groupBy:string|undefined):{label:string;items:WorkItem[]}[] {
  if(!groupBy) return [{label:"",items}];
  const groups=new Map<string,WorkItem[]>();
  for(const item of items) {const label=groupLabel(item,groupBy);const current=groups.get(label)??[];current.push(item);groups.set(label,current);}
  return Array.from(groups,([label,groupItems])=>({label,items:groupItems}));
}

function groupLabel(item:WorkItem,groupBy:string):string {
  switch(groupBy) {
    case "object_type":return item.object_type==="incident"?"Incydenty":"Zadania SCTASK";
    case "state":return stateLabel(item.state);
    case "state_category":return ({active:"Aktywne",waiting:"Oczekujące",complete:"Zakończone"} as Record<string,string>)[item.state_category]??item.state_category;
    case "priority":return item.priority?`P${item.priority}`:"Bez priorytetu";
    case "assignment_group_id":return item.group_name||"Bez grupy";
    default:return "Pozostałe";
  }
}

function ActionResults({results}:{results:ActionResult[]}) {
  if(!results.length)return null;
  return <section className="panel workspace-action-results" aria-live="polite"><h2>Wyniki akcji</h2><ul>{results.map((result)=><li key={referenceKey(result)}><strong>{result.object_type==="incident"?"INC":"SCTASK"} {result.object_id}:</strong> {result.status}{result.message?` · ${result.message}`:""}</li>)}</ul></section>;
}
