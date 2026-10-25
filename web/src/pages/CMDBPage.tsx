import { lazy, Suspense, useEffect, useRef, useState, type FormEvent } from "react";
import { Link, useLocation, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { ApiError, api, jsonBody } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { ConflictNotice } from "../components/ConflictNotice";
import { PageState } from "../components/PageState";
import { RemoteCombobox, type Choice } from "../components/RemoteCombobox";
import { useRemotePage } from "../components/useRemotePage";
import { useIncidentQuery, useIncidentTitleFocus } from "../incidents/hooks";
import { AssetPanel, InventoryHistory, LinkedRecords, PageFeedback, RelationsPanel } from "../cmdb/components";
import { cmdbAttributeLabel, cmdbLabel, cmdbText, useCMDBLocale } from "../cmdb/messages";
import type { Attribute, CI, Class, Metadata } from "../cmdb/contracts";
import { MetadataFields } from "../metadata/MetadataFields";
import { formatDateTime, formatMetadataValue } from "../metadata/format";

const CIProvenancePanel = lazy(() => import("./CMDBIREPage").then((module) => ({ default: module.CIProvenancePanel })));

const pageSize = "25";
const listPath = "/cmdb";

function t(key: string, locale: "pl" | "en") {
  return cmdbText(key, locale);
}

function ciDictionaryLabel(metadata: Metadata | undefined, key: string, value: string, locale: "pl" | "en") {
  const option = metadata?.custom_fields_schema?.configuration?.dictionaries[key]?.find((item) => item.value === value);
  return option?.labels[locale] || option?.labels.pl || cmdbLabel(value, locale);
}

function layoutKeys(schema: import("../metadata/contracts").MetadataSchema, defaults: string[], hasPermission: (key: string) => boolean) {
  const roleViews = Object.entries(schema.list.role_views ?? {}).filter(([permission]) => hasPermission(permission));
  const selected = roleViews.length ? [...new Set(roleViews.flatMap(([, keys]) => keys))] : defaults;
  return selected.filter((key) => schema.fields.some((field) => field.key === key && field.active));
}

const legacyListColumns = ["identifier", "name", "class", "status"];

function ciColumnLabel(key: string, locale: "pl" | "en") {
  const labels: Record<string, [string, string]> = {
    identifier: ["Identyfikator", "Identifier"],
    name: ["Nazwa", "Name"],
    class: ["Klasa", "Class"],
    status: ["Stan", "Status"],
    environment: ["Środowisko", "Environment"],
    owner_id: ["Właściciel", "Owner"],
    location: ["Lokalizacja", "Location"],
  };
  return labels[key]?.[locale === "en" ? 1 : 0] ?? key;
}

function canReadCMDB(hasPermission: (key: string) => boolean) {
  return hasPermission("cmdb.read") || hasPermission("cmdb.read_own");
}

function safeReturnPath(value: string | null) {
  if (!value || !(value === "/cmdb" || value.startsWith("/cmdb?") || value.startsWith("/cmdb/")) || value.startsWith("//")) return listPath;
  return value;
}

function carryReturn(path: string, returnTo: string) {
  const url = new URL(path, window.location.origin);
  url.searchParams.set("return", returnTo);
  return url.pathname + url.search;
}

export function CMDBListPage() {
  const locale = useCMDBLocale();
  const heading = useIncidentTitleFocus();
  const { hasPermission } = useAuth();
  const location = useLocation();
  const [params, setParams] = useSearchParams();
  const schema = useIncidentQuery<Metadata>("/api/v1/cmdb/classes");
  const metadataSchema = schema.data?.custom_fields_schema;
  const listColumns = metadataSchema
    ? layoutKeys(metadataSchema, metadataSchema.list.columns, hasPermission)
    : legacyListColumns;
  const configuredFilters = metadataSchema?.list.filters ?? [];
  const [queryDraft, setQueryDraft] = useState(params.get("q") ?? "");
  const [locationDraft, setLocationDraft] = useState(params.get("location") ?? "");
  const query = new URLSearchParams();
  for (const key of ["q", "class", "status", "environment", "archived", "location"]) {
    const value = key === "archived" ? params.get(key) ?? "false" : params.get(key) ?? "";
    if (key === "archived" && value === "any") continue;
    if (value) query.set(key, value);
  }
  for (const key of configuredFilters) {
    const value = params.get(`cf.${key}`);
    if (value) query.set(`cf.${key}`, value);
  }
  const defaultSort = metadataSchema?.list.default_sort ?? "newest";
  if (defaultSort !== "newest") query.set("sort", defaultSort);
  query.set("limit", pageSize);
  const page = useRemotePage<CI>("/api/v1/cmdb/cis?" + query.toString(), "items", "before_id");
  const canRead = canReadCMDB(hasPermission);
  const canManage = hasPermission("cmdb.manage") && hasPermission("cmdb.read");
  const returnTo = location.pathname + location.search;

  useEffect(() => {
    setQueryDraft(params.get("q") ?? "");
    setLocationDraft(params.get("location") ?? "");
  }, [params]);

  function setFilter(key: string, value: string) {
    const next = new URLSearchParams(params);
    next.delete("before_id");
    value ? next.set(key, value) : next.delete(key);
    setParams(next);
  }

  function submitFilters(event: FormEvent) {
    event.preventDefault();
    const next = new URLSearchParams(params);
    next.delete("before_id");
    const q = queryDraft.trim();
    const locationValue = locationDraft.trim();
    q ? next.set("q", q) : next.delete("q");
    locationValue ? next.set("location", locationValue) : next.delete("location");
    setParams(next);
  }

  function resetFilters() {
    setQueryDraft("");
    setLocationDraft("");
    setParams(new URLSearchParams());
  }

  function filterCustom(values: Record<string, unknown>) {
    const next = new URLSearchParams(params);
    next.delete("before_id");
    for (const key of configuredFilters) {
      const value = values[`cf.${key}`];
      const encoded = value === undefined || value === null ? "" : String(value);
      if (encoded) next.set(`cf.${key}`, encoded);
      else next.delete(`cf.${key}`);
    }
    setParams(next);
  }

  if (!canRead) return <PageState title={t("noPermission", locale)} detail={t("noPermissionDetail", locale)} tone="error" />;
  if (schema.error) return <section className="page"><PageState title={t("loadError", locale)} detail={schema.error} tone="error" /><button className="button button--secondary mt-3" onClick={schema.reload}>{t("retry", locale)}</button></section>;

  return (
    <section className="page">
      <div className="page-heading">
        <div>
          <p className="eyebrow">CMDB</p>
          <h1 ref={heading} tabIndex={-1}>{t("title", locale)}</h1>
          <p className="muted">{t("subtitle", locale)}</p>
        </div>
        <div className="action-row">
          {hasPermission("cmdb.import") && (canManage || hasPermission("assets.manage")) && <Link className="button button--secondary" to="/cmdb/imports">{t("imports", locale)}</Link>}
          {canManage && <Link className="button button--primary" to={carryReturn("/cmdb/new", returnTo)}>{t("create", locale)}</Link>}
        </div>
      </div>

      <form className="panel form-grid" onSubmit={submitFilters}>
        <label>
          {t("search", locale)}
          <input value={queryDraft} maxLength={160} onChange={(event) => setQueryDraft(event.target.value)} />
          <small className="muted">{t("searchHint", locale)}</small>
        </label>
        <label>
          {t("class", locale)}
          <select value={params.get("class") ?? ""} disabled={schema.loading || !schema.data} onChange={(event) => setFilter("class", event.target.value)}>
            <option value="">{t("all", locale)}</option>
            {schema.data?.classes.map((item) => <option key={item.key} value={item.key}>{cmdbLabel(item.key, locale)}</option>)}
          </select>
        </label>
        <label>
          {t("status", locale)}
          <select value={params.get("status") ?? ""} disabled={schema.loading || !schema.data} onChange={(event) => setFilter("status", event.target.value)}>
            <option value="">{t("all", locale)}</option>
            {schema.data?.statuses.map((value) => <option key={value} value={value}>{ciDictionaryLabel(schema.data, "ci.status", value, locale)}</option>)}
          </select>
        </label>
        <label>
          {t("environment", locale)}
          <select value={params.get("environment") ?? ""} disabled={schema.loading || !schema.data} onChange={(event) => setFilter("environment", event.target.value)}>
            <option value="">{t("all", locale)}</option>
            {schema.data?.environments.map((value) => <option key={value} value={value}>{ciDictionaryLabel(schema.data, "ci.environment", value, locale)}</option>)}
          </select>
        </label>
        <label>
          {t("archived", locale)}
          <select value={params.get("archived") ?? "false"} onChange={(event) => setFilter("archived", event.target.value)}>
            <option value="false">{t("activeRecords", locale)}</option>
            <option value="true">{t("archivedRecords", locale)}</option>
            <option value="any">{t("all", locale)}</option>
          </select>
        </label>
        <label>
          {t("location", locale)}
          <input value={locationDraft} maxLength={160} onChange={(event) => setLocationDraft(event.target.value)} />
        </label>
        {metadataSchema && <MetadataFields
          schema={metadataSchema}
          values={Object.fromEntries([...params.entries()].filter(([key]) => key.startsWith("cf.")))}
          onChange={filterCustom}
          locale={locale}
          filterable
        />}
        <div className="action-row">
          <button className="button button--primary" type="submit">{t("applyFilters", locale)}</button>
          <button className="button button--secondary" type="button" onClick={resetFilters}>{t("resetFilters", locale)}</button>
        </div>
      </form>

      {schema.loading && <PageState title={t("loading", locale)} />}
      <PageFeedback loading={page.loading} error={page.error} empty={false} />
      {page.error && <button className="button button--secondary mt-3" onClick={page.retry}>{t("retry", locale)}</button>}
      {!page.loading && !page.error && page.items.length === 0 && (
        <div className="panel">
          <PageState title={t("noRecords", locale)} detail={t("noRecordsDetail", locale)} />
          {canManage && <Link className="button button--primary mt-3" to={carryReturn("/cmdb/new", returnTo)}>{t("create", locale)}</Link>}
        </div>
      )}
      {page.items.length > 0 && (
        <div className="table-wrap">
          <table>
            <thead><tr>{listColumns.map((key) => { const field = metadataSchema?.fields.find((item) => item.key === key); return <th key={key}>{field?.labels[locale] || field?.labels.pl || ciColumnLabel(key, locale)}</th>; })}</tr></thead>
            <tbody>
              {page.items.map((record) => (
                <tr key={record.id}>
                  {listColumns.map((key) => { const field = metadataSchema?.fields.find((item) => item.key === key); return <td key={key}>{key === "identifier" ? <><Link to={carryReturn("/cmdb/" + record.id, returnTo)}>{record.identifier}</Link>{record.archived && <span className="ml-2 text-xs text-amber-700">{t("archivedRecords", locale)}</span>}</> : key === "name" ? record.name : key === "class" ? cmdbLabel(record.class, locale) : key === "status" ? ciDictionaryLabel(schema.data, "ci.status", record.status, locale) : key === "environment" ? ciDictionaryLabel(schema.data, "ci.environment", record.environment, locale) : key === "owner_id" ? record.owner_name : key === "location" ? record.location || "—" : displayCustomValue(record.custom_fields?.[key], locale, field?.type)}</td>; })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {page.next && <button className="button button--secondary mt-4" disabled={page.loading} onClick={() => void page.more()}>{locale === "en" ? "Load more" : "Pokaż więcej"}</button>}
    </section>
  );
}

export function CMDBCreatePage() {
  const locale = useCMDBLocale();
  const { hasPermission } = useAuth();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const schema = useIncidentQuery<Metadata>("/api/v1/cmdb/classes");
  const returnTo = safeReturnPath(params.get("return"));
  if (!canReadCMDB(hasPermission) || !hasPermission("cmdb.manage") || !hasPermission("cmdb.read")) {
    return <PageState title={t("createNeedsPermission", locale)} tone="error" />;
  }
  if (schema.loading) return <PageState title={t("loading", locale)} />;
  if (schema.error || !schema.data) return <section className="page"><PageState title={t("loadError", locale)} detail={schema.error} tone="error" />{schema.error && <button className="button button--secondary mt-3" onClick={schema.reload}>{t("retry", locale)}</button>}</section>;
  return (
    <section className="page">
      <div className="page-heading">
        <div><p className="eyebrow"><Link to={returnTo}>{t("backToList", locale)}</Link></p><h1>{t("newTitle", locale)}</h1></div>
      </div>
      <CIEditor
        key="new"
        metadata={schema.data}
        onCancel={() => navigate(returnTo)}
        onSaved={(record) => navigate(carryReturn("/cmdb/" + record.id, returnTo), { replace: true })}
      />
    </section>
  );
}

type Draft = Pick<CI, "identifier" | "name" | "class" | "status" | "owner_id" | "environment" | "location"> & {
  attributes: Record<string, unknown>;
  custom_fields: Record<string, unknown>;
  custom_fields_version: string;
};

function makeDraft(metadata: Metadata, record?: CI, owner?: Choice): Draft {
  if (record) {
    return {
      identifier: record.identifier,
      name: record.name,
      class: record.class,
      status: record.status,
      owner_id: record.owner_id,
      environment: record.environment,
      location: record.location,
      attributes: { ...record.attributes },
      custom_fields: { ...(record.custom_fields ?? {}) },
      custom_fields_version: record.custom_fields_version,
    };
  }
  return {
    identifier: "",
    name: "",
    class: metadata.classes[0]?.key ?? "",
    status: metadata.statuses[0] ?? "",
    owner_id: owner?.id ?? "",
    environment: metadata.environments[0] ?? "",
    location: "",
    attributes: {},
    custom_fields: {},
    custom_fields_version: metadata.custom_fields_schema.version,
  };
}

function CIEditor({
  metadata,
  record,
  onCancel,
  onSaved,
}: {
  metadata: Metadata;
  record?: CI;
  onCancel: () => void;
  onSaved: (record: CI) => void;
}) {
  const locale = useCMDBLocale();
  const { session } = useAuth();
  const ownerInitial = record
    ? { id: record.owner_id, label: record.owner_name }
    : session?.user
      ? { id: session.user.id, label: session.user.display_name }
      : null;
  const [owner, setOwner] = useState<Choice | null>(ownerInitial);
  const [draft, setDraft] = useState<Draft>(() => makeDraft(metadata, record, ownerInitial ?? undefined));
  const [base, setBase] = useState<Draft>(() => makeDraft(metadata, record, ownerInitial ?? undefined));
  const [version, setVersion] = useState(record?.version ?? "0");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [latest, setLatest] = useState<CI | null>(null);
  const [reloadError, setReloadError] = useState<string>();
  const request = useRef<AbortController | null>(null);
  const pending = useRef(false);
  const operation = useRef<{ signature: string; key: string } | null>(null);
  const classDef = metadata.classes.find((item) => item.key === draft.class);
  const attributeFields = classDef?.attributes ?? [];

  useEffect(() => () => request.current?.abort(), []);

  function update(field: keyof Draft, value: string) {
    setDraft((current) => ({ ...current, [field]: value }));
  }

  function updateClass(value: string) {
    const allowed = new Set(metadata.classes.find((item) => item.key === value)?.attributes.map((field) => field.key) ?? []);
    setDraft((current) => ({
      ...current,
      class: value,
      attributes: Object.fromEntries(Object.entries(current.attributes).filter(([key]) => allowed.has(key))),
    }));
  }

  function updateAttribute(field: Attribute, value: string) {
    setDraft((current) => {
      const attributes = { ...current.attributes };
      if (value === "") delete attributes[field.key];
      else attributes[field.key] = field.type === "INTEGER" || field.type === "NUMBER" ? Number(value) : value;
      return { ...current, attributes };
    });
  }

  function mergeDraft(current: CI): Draft {
    const currentDraft = makeDraft(metadata, current, { id: current.owner_id, label: current.owner_name });
    const merged = { ...currentDraft };
    const scalarFields: (keyof Draft)[] = ["identifier", "name", "class", "status", "owner_id", "environment", "location"];
    for (const field of scalarFields) {
      if (draft[field] !== base[field]) Object.assign(merged, { [field]: draft[field] });
    }
    const attributes = { ...currentDraft.attributes };
    const keys = new Set([...Object.keys(base.attributes), ...Object.keys(draft.attributes)]);
    for (const key of keys) {
      if (JSON.stringify(base.attributes[key]) === JSON.stringify(draft.attributes[key])) continue;
      if (Object.prototype.hasOwnProperty.call(draft.attributes, key)) attributes[key] = draft.attributes[key];
      else delete attributes[key];
    }
    merged.attributes = attributes;
    const customFields = { ...currentDraft.custom_fields };
    const customKeys = new Set([...Object.keys(base.custom_fields), ...Object.keys(draft.custom_fields)]);
    for (const key of customKeys) {
      if (JSON.stringify(base.custom_fields[key]) === JSON.stringify(draft.custom_fields[key])) continue;
      if (Object.prototype.hasOwnProperty.call(draft.custom_fields, key)) customFields[key] = draft.custom_fields[key];
      else delete customFields[key];
    }
    merged.custom_fields = customFields;
    return merged;
  }

  async function fetchLatest() {
    if (!record) return;
    setReloadError(undefined);
    try {
      const current = await api<CI>("/api/v1/cmdb/cis/" + record.id);
      setLatest(current);
      return current;
    } catch (cause) {
      setReloadError(cause instanceof Error ? cause.message : String(cause));
      return null;
    }
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (pending.current || record?.archived || !owner) return;
    pending.current = true;
    setBusy(true);
    setError(undefined);
    setReloadError(undefined);
    setLatest(null);
    const body = { ...draft, owner_id: owner.id, version };
    const signature = JSON.stringify(body);
    const headers: Record<string, string> = {};
    if (!record) {
      if (!operation.current || operation.current.signature !== signature) {
        operation.current = { signature, key: crypto.randomUUID() };
      }
      headers["Idempotency-Key"] = operation.current.key;
    }
    const controller = new AbortController();
    request.current?.abort();
    request.current = controller;
    try {
      const saved = await api<CI>(record ? "/api/v1/cmdb/cis/" + record.id : "/api/v1/cmdb/cis", {
        method: record ? "PUT" : "POST",
        body: jsonBody(body),
        headers,
        signal: controller.signal,
      });
      onSaved(saved);
    } catch (cause) {
      if (controller.signal.aborted) return;
      setError(cause instanceof Error ? cause.message : String(cause));
      if (record && cause instanceof ApiError && cause.status === 409) await fetchLatest();
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }

  function useLatest() {
    if (!latest) return;
    const next = makeDraft(metadata, latest, { id: latest.owner_id, label: latest.owner_name });
    setDraft(next);
    setBase(next);
    setOwner({ id: latest.owner_id, label: latest.owner_name });
    setVersion(latest.version);
    setLatest(null);
    setError(undefined);
  }

  function keepDraft() {
    if (!latest) return;
    const merged = mergeDraft(latest);
    setDraft(merged);
    setBase(makeDraft(metadata, latest, { id: latest.owner_id, label: latest.owner_name }));
    setOwner({ id: merged.owner_id, label: merged.owner_id === latest.owner_id ? latest.owner_name : owner?.label ?? merged.owner_id });
    setVersion(latest.version);
    setLatest(null);
    setError(undefined);
  }

  return (
    <form className="panel form-grid" onSubmit={submit}>
      {error && <PageState title={locale === "en" ? "Save failed" : "Nie udało się zapisać"} detail={error} tone="error" />}
      {record?.archived && <PageState title={t("archivedEdit", locale)} tone="error" />}
      {latest && (
        <ConflictNotice
          current={<><p>{latest.identifier} · {latest.name}</p><p>{cmdbLabel(latest.class, locale)} · {cmdbLabel(latest.status, locale)} · {latest.owner_name}</p><pre>{JSON.stringify(latest.attributes, null, 2)}</pre></>}
          proposal={<><p>{draft.identifier} · {draft.name}</p><p>{cmdbLabel(draft.class, locale)} · {cmdbLabel(draft.status, locale)} · {owner?.label ?? draft.owner_id}</p><pre>{JSON.stringify(draft.attributes, null, 2)}</pre></>}
          acceptCurrent={useLatest}
          keepProposal={keepDraft}
        />
      )}
      {reloadError && <PageState title={t("versionConflict", locale)} detail={reloadError} tone="error" />}
      {latest && <p className="text-sm text-amber-800">{t("mergeWarning", locale)}</p>}
      {metadata.ci_fields.map((field) => (
        <FormField
          key={field.key}
          field={field}
          value={draft[field.key as keyof Draft]}
          owner={owner}
          onOwnerChange={(next) => {
            setOwner(next);
            update("owner_id", next?.id ?? "");
          }}
          onChange={(value) => field.key === "class" ? updateClass(value) : update(field.key as keyof Draft, value)}
          disabled={busy || Boolean(record && field.key === "class")}
          locale={locale}
        />
      ))}
      {classDef && <fieldset className="form-grid border-t border-slate-200 pt-4">
        <legend className="font-semibold">{t("attributes", locale)} · {cmdbLabel(classDef.key, locale)}</legend>
        {attributeFields.length === 0 && <p className="muted">{t("noAttributes", locale)}</p>}
        {attributeFields.map((field) => (
          <FormField
            key={field.key}
            field={field}
            value={draft.attributes[field.key]}
            onChange={(value) => updateAttribute(field, value)}
            disabled={busy || Boolean(latest)}
            locale={locale}
          />
        ))}
      </fieldset>}
      <MetadataFields
        schema={metadata.custom_fields_schema}
        values={draft.custom_fields}
        baseValues={{ identifier: draft.identifier, name: draft.name, class: draft.class, status: draft.status, owner_id: owner?.id ?? draft.owner_id, environment: draft.environment, location: draft.location }}
        onChange={(custom_fields) => setDraft((current) => ({ ...current, custom_fields }))}
        locale={locale}
        disabled={busy || Boolean(latest)}
      />
      <div className="action-row">
        <button className="button button--primary" disabled={busy || Boolean(latest) || Boolean(record?.archived) || !owner} type="submit">{busy ? t("saving", locale) : t("save", locale)}</button>
        <button className="button button--secondary" disabled={busy} type="button" onClick={onCancel}>{t("cancel", locale)}</button>
      </div>
    </form>
  );
}

function FormField({
  field,
  value,
  owner,
  onOwnerChange,
  onChange,
  disabled,
  locale,
}: {
  field: Attribute;
  value: unknown;
  owner?: Choice | null;
  onOwnerChange?: (choice: Choice | null) => void;
  onChange: (value: string) => void;
  disabled: boolean;
  locale: "pl" | "en";
}) {
  const label = field.type === "USER_REFERENCE"
    ? t("owner_id", locale)
    : ["identifier", "name", "class", "status", "environment", "location"].includes(field.key)
      ? t(field.key, locale)
      : cmdbAttributeLabel(field.key, field.label, locale);
  if (field.type === "USER_REFERENCE") {
    return <div className="form-field"><RemoteCombobox kind="users" label={label + (field.required ? " *" : "")} value={owner ?? null} onChange={onOwnerChange ?? (() => undefined)} endpoint="/api/v1/cmdb/lookups/users" collection="items" cursor="before_id" maxQueryLength={254} disabled={disabled} /><small>{t("noOwner", locale)}</small></div>;
  }
  if (field.type === "SELECT") {
    return <label>{label}{field.required ? " *" : ""}<select required={field.required} value={String(value ?? "")} disabled={disabled} onChange={(event) => onChange(event.target.value)}><option value="">{t("select", locale)}</option>{(field.options ?? []).map((option) => <option key={option} value={option}>{cmdbLabel(option, locale)}</option>)}</select></label>;
  }
  const numeric = field.type === "INTEGER" || field.type === "NUMBER";
  const type = field.type === "URL" ? "url" : numeric ? "number" : "text";
  return (
    <label>
      {label}{field.required ? " *" : ""}
      <input
        type={type}
        required={field.required}
        maxLength={field.max_length || undefined}
        min={numeric ? field.min : undefined}
        max={numeric ? field.max : undefined}
        step={field.type === "INTEGER" ? 1 : field.type === "NUMBER" ? "any" : undefined}
        value={value === undefined || value === null ? "" : String(value)}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value)}
      />
    </label>
  );
}

const tabs = ["overview", "relations", "asset", "records", "history", "provenance"] as const;
type TabKey = (typeof tabs)[number];

export function CMDBDetailPage() {
  const locale = useCMDBLocale();
  const { ciID } = useParams();
  const { hasPermission } = useAuth();
  const [params, setParams] = useSearchParams();
  const returnTo = safeReturnPath(params.get("return"));
  const query = useIncidentQuery<CI>(ciID ? "/api/v1/cmdb/cis/" + ciID : null);
  const schema = useIncidentQuery<Metadata>("/api/v1/cmdb/classes");
  const [record, setRecord] = useState<CI>();
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [notice, setNotice] = useState<string>();
  const [archiveReason, setArchiveReason] = useState("");
  const pending = useRef(false);
  const heading = useIncidentTitleFocus(ciID);
  const requestedTab = params.get("tab") as TabKey | null;
  const active: TabKey = requestedTab && tabs.includes(requestedTab) ? requestedTab : "overview";
  const canManage = hasPermission("cmdb.manage") && hasPermission("cmdb.read");

  useEffect(() => {
    if (query.data) setRecord(query.data);
  }, [query.data]);

  function selectTab(tab: TabKey) {
    const next = new URLSearchParams(params);
    next.set("tab", tab);
    setParams(next);
    setEditing(false);
  }

  async function save(path: string, body: unknown, method = "POST") {
    if (pending.current) return false;
    pending.current = true;
    setBusy(true);
    setError(undefined);
    setNotice(undefined);
    try {
      await api<unknown>(path, { method, body: jsonBody(body) });
      setNotice(locale === "en" ? "Saved." : "Zapisano.");
      try {
        setRecord(await api<CI>("/api/v1/cmdb/cis/" + record!.id));
      } catch (cause) {
        query.reload();
        setNotice(locale === "en"
          ? `Saved, but the current record could not be refreshed: ${cause instanceof Error ? cause.message : String(cause)}`
          : `Zapisano, ale nie udało się odświeżyć rekordu: ${cause instanceof Error ? cause.message : String(cause)}`);
      }
      return true;
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
      if (cause instanceof ApiError && cause.status === 409) {
        try {
          setRecord(await api<CI>("/api/v1/cmdb/cis/" + ciID));
        } catch {
          // Keep the selector draft and show the original version conflict.
        }
      }
      return false;
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }

  async function archive(archived: boolean) {
    if (!record || pending.current) return;
    pending.current = true;
    setBusy(true);
    setError(undefined);
    setNotice(undefined);
    try {
      const updated = await api<CI>("/api/v1/cmdb/cis/" + record.id + "/archive", {
        method: "POST",
        body: jsonBody({ version: record.version, archived, reason: archiveReason }),
      });
      setRecord(updated);
      setArchiveReason("");
      setNotice(archived ? (locale === "en" ? "CI archived." : "Zarchiwizowano CI.") : (locale === "en" ? "CI restored." : "Przywrócono CI."));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
      if (cause instanceof ApiError && cause.status === 409) {
        try {
          setRecord(await api<CI>("/api/v1/cmdb/cis/" + record.id));
        } catch {
          query.reload();
        }
      }
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }

  if (!canReadCMDB(hasPermission)) return <PageState title={t("noPermission", locale)} detail={t("noPermissionDetail", locale)} tone="error" />;
  if (query.loading || (!record && !query.error)) return <PageState title={t("loading", locale)} />;
  if (query.error || !record) return <section className="page"><PageState title={t("loadError", locale)} detail={query.error} tone="error" />{query.error && <button className="button button--secondary mt-3" onClick={query.reload}>{t("retry", locale)}</button>}</section>;

  return (
    <section className="page">
      <div className="page-heading">
        <div>
          <p className="eyebrow"><Link to={returnTo}>{t("backToList", locale)}</Link></p>
          <h1 ref={heading} tabIndex={-1}>{record.name}</h1>
          <p className="muted">{record.identifier} · {cmdbLabel(record.class, locale)} · {record.archived ? t("archivedRecords", locale) : cmdbLabel(record.status, locale)}</p>
        </div>
        {canManage && <div className="action-row">
          {!record.archived && <button className="button button--secondary" disabled={busy} onClick={() => { selectTab("overview"); setEditing((value) => !value); }}>{editing ? t("cancel", locale) : t("edit", locale)}</button>}
          <button className="button button--secondary" disabled={busy || archiveReason.trim().length === 0} onClick={() => void archive(!record.archived)}>{record.archived ? t("restore", locale) : t("archive", locale)}</button>
        </div>}
      </div>
      {error && <PageState title={locale === "en" ? "Operation failed" : "Operacja nie powiodła się"} detail={error} tone="error" />}
      {notice && <PageState title={notice} tone="success" />}
      {record.archived && <PageState title={t("archiveReadOnly", locale)} />}
      {canManage && <div className="panel form-grid">
        <label>{t("archiveReason", locale)}<input maxLength={1000} value={archiveReason} disabled={busy} onChange={(event) => setArchiveReason(event.target.value)} /></label>
      </div>}
      <div className="cmdb-tabs" role="tablist" aria-label={t("detail", locale)}>
        {tabs.map((tab) => <button key={tab} id={`cmdb-tab-${tab}`} type="button" role="tab" aria-controls={`cmdb-panel-${tab}`} aria-selected={active === tab} tabIndex={active === tab ? 0 : -1} onClick={() => selectTab(tab)}>{tab === "provenance" ? (locale === "en" ? "Data provenance" : "Pochodzenie danych") : t(tab, locale)}</button>)}
      </div>
      <div id={`cmdb-panel-${active}`} role="tabpanel" aria-labelledby={`cmdb-tab-${active}`} tabIndex={0}>
      {active === "overview" && (editing && canManage ? (
        schema.data ? <CIEditor key={record.id} metadata={schema.data} record={record} onCancel={() => setEditing(false)} onSaved={(updated) => { setRecord(updated); setEditing(false); setNotice(locale === "en" ? "Saved." : "Zapisano."); }} /> : <PageFeedback loading={schema.loading} error={schema.error} empty={false} />
      ) : <CIOverview record={record} metadata={schema.data} locale={locale} />)}
      {active === "relations" && schema.data && <RelationsPanel record={record} metadata={schema.data} busy={busy} save={save} />}
      {active === "relations" && !schema.data && <><PageFeedback loading={schema.loading} error={schema.error} empty={false} />{schema.error && <button className="button button--secondary mt-3" onClick={schema.reload}>{t("retry", locale)}</button>}</>}
      {active === "asset" && <AssetPanel record={record} busy={busy} save={save} />}
      {active === "records" && <LinkedRecords id={record.id} />}
      {active === "history" && <InventoryHistory path={"/api/v1/cmdb/cis/" + record.id + "/history"} />}
      {active === "provenance" && <Suspense fallback={<PageFeedback loading error="" empty={false} />}><CIProvenancePanel ciID={record.id} version={record.version} canManage={canManage && !record.archived} onVersionChange={(version) => setRecord((current) => current ? { ...current, version } : current)} /></Suspense>}
      </div>
    </section>
  );
}

function CIOverview({ record, metadata, locale }: { record: CI; metadata?: Metadata; locale: "pl" | "en" }) {
  const classDef: Class | undefined = metadata?.classes.find((item) => item.key === record.class);
  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <section className="panel">
        <h2 className="text-lg font-semibold">{t("overview", locale)}</h2>
        <dl className="mt-4 grid gap-3 sm:grid-cols-2">
          <Summary label={t("identifier", locale)} value={record.identifier} />
          <Summary label={t("name", locale)} value={record.name} />
          <Summary label={t("class", locale)} value={cmdbLabel(record.class, locale) ?? record.class} />
          <Summary label={t("status", locale)} value={cmdbLabel(record.status, locale) ?? record.status} />
          <Summary label={t("environment", locale)} value={cmdbLabel(record.environment, locale) ?? record.environment} />
          <Summary label={t("owner_id", locale)} value={record.owner_name} />
          <Summary label={t("location", locale)} value={record.location || "—"} />
          <Summary label={t("created", locale)} value={formatDateTime(record.created_at, locale)} />
          <Summary label={t("updated", locale)} value={formatDateTime(record.updated_at, locale)} />
        </dl>
      </section>
      <section className="panel">
        <h2 className="text-lg font-semibold">{t("attributes", locale)}</h2>
        {!Object.keys(record.attributes ?? {}).length && <p className="muted mt-3">{t("noAttributes", locale)}</p>}
        <dl className="mt-4 grid gap-3 sm:grid-cols-2">
          {Object.entries(record.attributes ?? {}).map(([key, value]) => {
            const definition = classDef?.attributes.find((field) => field.key === key);
            return <Summary key={key} label={cmdbAttributeLabel(key, definition?.label ?? key, locale)} value={String(value)} />;
          })}
        </dl>
      </section>
      {metadata && Object.keys(record.custom_fields ?? {}).length > 0 && <section className="panel lg:col-span-2">
        <h2 className="text-lg font-semibold">{locale === "en" ? "Additional information" : "Informacje dodatkowe"}</h2>
        <dl className="mt-4 grid gap-3 sm:grid-cols-2">
          {metadata.custom_fields_schema.fields.filter((field) => field.active && !field.system && record.custom_fields?.[field.key] !== undefined).map((field) => <Summary key={field.key} label={field.labels[locale] || field.labels.pl || field.key} value={displayCustomValue(record.custom_fields?.[field.key], locale, field.type)} />)}
        </dl>
      </section>}
    </div>
  );
}

function Summary({ label, value }: { label: string; value: string }) {
  return <div><dt className="text-sm text-slate-500">{label}</dt><dd className="font-medium break-words">{value}</dd></div>;
}

function displayCustomValue(value: unknown, locale: "pl" | "en" = "pl", type?: string): string {
  return formatMetadataValue(value, locale, type);
}
