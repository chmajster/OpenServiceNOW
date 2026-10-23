import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { api, jsonBody } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { PageState } from "../components/PageState";
import { useCMDBLocale } from "../cmdb/messages";
import { formatDateTime } from "../metadata/format";
import type { Locale } from "../metadata/contracts";
import {
  type Availability,
  type ImpactResult,
  type LookupOption,
  type Page,
  type PortfolioItem,
  type PortfolioOffer,
  type PortfolioRelation,
  type PublicOffer,
  type RetirementImpact,
} from "../serviceportfolio/contracts";
import { portfolioText } from "../serviceportfolio/messages";

const base = "/api/v1/service-portfolio";
const itemTypes = ["BUSINESS_APPLICATION", "SERVICE_INSTANCE", "TECHNICAL_SERVICE", "BUSINESS_SERVICE"] as const;
const typeLabel: Record<string, [string, string]> = {
  BUSINESS_APPLICATION: ["Aplikacja biznesowa", "Business application"],
  SERVICE_INSTANCE: ["Instancja usługi", "Service instance"],
  TECHNICAL_SERVICE: ["Usługa techniczna", "Technical service"],
  BUSINESS_SERVICE: ["Usługa biznesowa", "Business service"],
};
const lifecycleLabel: Record<string, keyof ReturnType<typeof textKeys>> = {
  DRAFT: "draft", PUBLISHED: "published", ACTIVE: "active", RETIRED: "retired", WITHDRAWN: "withdrawn",
};
function textKeys() { return { draft: "draft", published: "published", active: "active", retired: "retired", withdrawn: "withdrawn" } as const; }
function t(key: Parameters<typeof portfolioText>[0], locale: Locale) { return portfolioText(key, locale); }
function displayType(value: string, locale: Locale) { return typeLabel[value]?.[locale === "en" ? 1 : 0] ?? value; }
function displayLifecycle(value: string, locale: Locale) { const key = lifecycleLabel[value]; return key ? t(key, locale) : value; }
function errorText(cause: unknown, fallback: string) { return cause instanceof Error && cause.message ? cause.message : fallback; }
function optionLabel(option: LookupOption) {
  const primary = option.name ?? option.service_key ?? option.identifier ?? option.key ?? option.id;
  const detail = option.email ?? option.identifier ?? (option.class ? `${option.class}${option.environment ? ` · ${option.environment}` : ""}` : "");
  return detail && detail !== primary ? `${primary} · ${detail}` : primary;
}
function selectedOption(id: string, name: string, extra: Partial<LookupOption> = {}): LookupOption | null {
  return id ? { id, name: name || id, ...extra } : null;
}

function LookupPicker({
  kind, label, value, onChange, disabled = false, endpoint,
}: {
  kind: string; label: string; value: LookupOption | null; onChange: (value: LookupOption | null) => void; disabled?: boolean; endpoint?: string;
}) {
  const locale = useCMDBLocale();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState("");
  const [options, setOptions] = useState<LookupOption[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const timer = window.setTimeout(() => setSearch(query.trim()), 220);
    return () => window.clearTimeout(timer);
  }, [query]);
  useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    setLoading(true);
    setError("");
    const url = new URL(endpoint ?? `${base}/lookups/${kind}`, window.location.origin);
    url.searchParams.set("limit", "30"); url.searchParams.set("q", search);
    void api<Page<LookupOption>>(`${url.pathname}${url.search}`, { signal: controller.signal }).then((page) => setOptions(page.items))
      .catch((cause: unknown) => { if (!controller.signal.aborted) { setOptions([]); setError(errorText(cause, t("loadError", locale))); } })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [endpoint, kind, locale, open, search]);
  return <div className="sp-lookup" ref={root} onBlur={(event) => { if (!root.current?.contains(event.relatedTarget)) setOpen(false); }}>
    <label>{label}
      <input
        role="combobox" aria-autocomplete="list" aria-expanded={open} autoComplete="off" disabled={disabled}
        value={open ? query : value ? optionLabel(value) : ""}
        onFocus={() => { setOpen(true); setQuery(""); setSearch(""); }}
        onChange={(event) => { setOpen(true); setQuery(event.target.value); onChange(null); }}
        onKeyDown={(event) => { if (event.key === "Escape") setOpen(false); }}
      />
    </label>
    {value && !open ? <button type="button" className="button button--small button--secondary sp-clear" aria-label={`${t("cancel", locale)}: ${label}`} onClick={() => onChange(null)}>×</button> : null}
    {open ? <div className="sp-lookup-options" role="listbox" aria-label={label}>
      {loading ? <span className="muted">{t("loading", locale)}</span> : null}
      {!loading && error ? <span role="alert">{error}</span> : null}
      {!loading && !error && options.length === 0 ? <span className="muted">{t("noResults", locale)}</span> : null}
      {options.map((option) => <button
        type="button" role="option" aria-selected={value?.id === option.id} key={option.id}
        onMouseDown={(event) => event.preventDefault()}
        disabled={option.active === false || option.lifecycle === "RETIRED"}
        onClick={() => { onChange(option); setOpen(false); setQuery(""); }}
      >{optionLabel(option)}{option.version ? <small>v{option.version}</small> : null}</button>)}
    </div> : null}
  </div>;
}

type Draft = {
  service_key: string; name: string; type: PortfolioItem["type"]; description: string; criticality: string;
  environment: PortfolioItem["environment"]; scope: string; dependency_mode: PortfolioItem["dependency_mode"];
  dependency_quorum: string;
};
type Picks = { business: LookupOption | null; technical: LookupOption | null; group: LookupOption | null; org: LookupOption | null; department: LookupOption | null; cost: LookupOption | null; location: LookupOption | null; ci: LookupOption | null };
const emptyDraft: Draft = { service_key: "", name: "", type: "BUSINESS_SERVICE", description: "", criticality: "MEDIUM", environment: "ALL", scope: "", dependency_mode: "NOT_MODELED", dependency_quorum: "" };

function PortfolioItemForm({ item, onSaved, onCancel }: { item?: PortfolioItem; onSaved: (item: PortfolioItem) => void; onCancel?: () => void }) {
  const locale = useCMDBLocale();
  const [draft, setDraft] = useState<Draft>(() => item ? {
    service_key: item.service_key, name: item.name, type: item.type, description: item.description,
    criticality: item.criticality, environment: item.environment, scope: item.scope,
    dependency_mode: item.dependency_mode, dependency_quorum: item.dependency_quorum ? String(item.dependency_quorum) : "",
  } : emptyDraft);
  const [picks, setPicks] = useState<Picks>(() => ({
    business: selectedOption(item?.business_owner_id ?? "", item?.business_owner_name ?? "", { email: undefined }),
    technical: selectedOption(item?.technical_owner_id ?? "", item?.technical_owner_name ?? ""),
    group: selectedOption(item?.support_group_id ?? "", item?.support_group_name ?? ""),
    org: selectedOption(item?.org_unit_ref_id ?? "", item?.org_unit_name ?? ""),
    department: selectedOption(item?.department_ref_id ?? "", item?.department_name ?? ""),
    cost: selectedOption(item?.cost_center_ref_id ?? "", item?.cost_center_name ?? ""),
    location: selectedOption(item?.location_id ?? "", item?.location_name ?? ""),
    ci: selectedOption(item?.ci_id ?? "", item?.ci_name ?? "", { version: item?.ci_version, class: item?.ci_class }),
  }));
  const [clearCILink, setClearCILink] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const retryKey = useRef("");
  const { hasPermission } = useAuth();
  const canReadCI = hasPermission("cmdb.read") || hasPermission("cmdb.read_own");
  function update<K extends keyof Draft>(key: K, value: Draft[K]) { retryKey.current = ""; setDraft((current) => ({ ...current, [key]: value })); }
  function updatePick(key: keyof Picks, value: LookupOption | null) { retryKey.current = ""; setPicks((current) => ({ ...current, [key]: value })); }
  async function submit(event: FormEvent) {
    event.preventDefault(); setBusy(true); setError("");
    const body = {
      version: item?.version ?? "0", service_key: draft.service_key, name: draft.name, type: draft.type,
      description: draft.description, criticality: draft.criticality, environment: draft.environment,
      scope: draft.scope, business_owner_id: picks.business?.id ?? "0", technical_owner_id: picks.technical?.id ?? "0",
      support_group_id: picks.group?.id ?? "0", org_unit_ref_id: picks.org?.id ?? "0",
      department_ref_id: picks.department?.id ?? "0", cost_center_ref_id: picks.cost?.id ?? "0",
      location_id: picks.location?.id ?? "0", ci_id: picks.ci?.id ?? "0", ci_version: picks.ci?.version ?? "0", clear_ci_link: clearCILink,
      dependency_mode: draft.dependency_mode, dependency_quorum: draft.dependency_mode === "QUORUM" ? Number(draft.dependency_quorum) : 0,
    };
    try {
      if (!item && !retryKey.current) retryKey.current = crypto.randomUUID();
      const result = await api<PortfolioItem>(item ? `${base}/items/${item.id}` : `${base}/items`, {
        method: item ? "PUT" : "POST", body: jsonBody(body),
        headers: item ? undefined : { "Idempotency-Key": retryKey.current },
      });
      onSaved(result);
    } catch (cause) { setError(errorText(cause, t("saveError", locale))); }
    finally { setBusy(false); }
  }
  return <form className="panel form-stack sp-form" onSubmit={(event) => void submit(event)}>
    <div className="panel-heading"><h2>{item ? t("detail", locale) : t("newItem", locale)}</h2>{onCancel ? <button className="button button--secondary" type="button" onClick={onCancel}>{t("cancel", locale)}</button> : null}</div>
    {error ? <PageState title={error} tone="error" /> : null}
    <div className="sp-form-grid">
      <label>{t("key", locale)}<input required minLength={2} maxLength={64} pattern="[A-Za-z0-9][A-Za-z0-9._-]{1,63}" value={draft.service_key} onChange={(e) => update("service_key", e.target.value.toUpperCase())} disabled={Boolean(item)} /></label>
      <label>{t("name", locale)}<input required maxLength={160} value={draft.name} onChange={(e) => update("name", e.target.value)} /></label>
      <label>{t("type", locale)}<select value={draft.type} disabled={Boolean(item)} onChange={(e) => update("type", e.target.value as Draft["type"])}>{itemTypes.map((type) => <option value={type} key={type}>{displayType(type, locale)}</option>)}</select></label>
      <label>{t("criticality", locale)}<select value={draft.criticality} onChange={(e) => update("criticality", e.target.value)}>{["CRITICAL", "HIGH", "MEDIUM", "LOW"].map((value) => <option key={value}>{value}</option>)}</select></label>
      <label>{t("environment", locale)}<select value={draft.environment} onChange={(e) => update("environment", e.target.value as Draft["environment"])}>{["DEV", "TEST", "STAGING", "PROD", "ALL"].map((value) => <option key={value}>{value}</option>)}</select></label>
      <label>{t("scope", locale)}<input maxLength={200} value={draft.scope} onChange={(e) => update("scope", e.target.value)} /></label>
      <LookupPicker kind="users" label={t("businessOwner", locale)} value={picks.business} onChange={(v) => updatePick("business", v)} />
      <LookupPicker kind="users" label={t("technicalOwner", locale)} value={picks.technical} onChange={(v) => updatePick("technical", v)} />
      <LookupPicker kind="groups" label={t("supportGroup", locale)} value={picks.group} onChange={(v) => updatePick("group", v)} />
      <LookupPicker kind="org_units" label={t("orgUnit", locale)} value={picks.org} onChange={(v) => updatePick("org", v)} />
      <LookupPicker kind="departments" label={t("department", locale)} value={picks.department} onChange={(v) => updatePick("department", v)} />
      <LookupPicker kind="cost_centers" label={t("costCenter", locale)} value={picks.cost} onChange={(v) => updatePick("cost", v)} />
      <LookupPicker kind="locations" label={t("location", locale)} value={picks.location} onChange={(v) => updatePick("location", v)} />
      {canReadCI ? <LookupPicker kind="cis" label={t("backingCI", locale)} value={picks.ci} onChange={(v) => { updatePick("ci", v); setClearCILink(Boolean(item?.ci_id) && !v); }} /> : null}
      <label>{t("dependencyMode", locale)}<select value={draft.dependency_mode} onChange={(e) => update("dependency_mode", e.target.value as Draft["dependency_mode"])}>
        <option value="NOT_MODELED">{t("notModeled", locale)}</option><option value="ALL">{t("allRequired", locale)}</option><option value="ANY">{t("anySufficient", locale)}</option><option value="QUORUM">QUORUM</option>
      </select></label>
      {draft.dependency_mode === "QUORUM" ? <label>{t("quorum", locale)}<input type="number" min={1} max={100} required value={draft.dependency_quorum} onChange={(e) => update("dependency_quorum", e.target.value)} /></label> : null}
    </div>
    <label>{t("description", locale)}<textarea rows={4} maxLength={4000} value={draft.description} onChange={(e) => update("description", e.target.value)} /></label>
    <div className="sp-actions"><button className="button button--primary" type="submit" disabled={busy}>{busy ? t("saving", locale) : t("save", locale)}</button></div>
  </form>;
}

export function ServicePortfolioListPage() {
  const locale = useCMDBLocale();
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const [items, setItems] = useState<PortfolioItem[]>([]);
  const [next, setNext] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  const { hasPermission } = useAuth();
  const canManage = hasPermission("service_portfolio.manage");
  const q = params.get("q") ?? "";
  const type = params.get("type") ?? "";
  const lifecycle = params.get("lifecycle") ?? "";
  const environment = params.get("environment") ?? "";
  const [queryDraft, setQueryDraft] = useState(q);
  useEffect(() => { setQueryDraft(q); }, [q]);
  const url = useMemo(() => {
    const query = new URLSearchParams({ limit: "25" });
    for (const [key, value] of [["q", q], ["type", type], ["lifecycle", lifecycle], ["environment", environment]] as const) if (value) query.set(key, value);
    return `${base}/items?${query}`;
  }, [environment, lifecycle, q, type]);
  const load = useCallback((path = url, append = false) => {
    const controller = new AbortController();
    setLoading(true); setError("");
    void api<Page<PortfolioItem>>(path, { signal: controller.signal }).then((page) => {
      setItems((current) => append ? [...current, ...page.items] : page.items);
      setNext(page.next_before_id ?? "");
    }).catch((cause: unknown) => { if (!controller.signal.aborted) setError(errorText(cause, t("loadError", locale))); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [locale, url]);
  useEffect(() => load(), [load]);
  function applyFilters(event: FormEvent) {
    event.preventDefault(); const nextParams = new URLSearchParams();
    if (queryDraft.trim()) nextParams.set("q", queryDraft.trim());
    const form = event.currentTarget as HTMLFormElement;
    const data = new FormData(form);
    for (const key of ["type", "lifecycle", "environment"]) { const value = String(data.get(key) ?? ""); if (value) nextParams.set(key, value); }
    setParams(nextParams);
  }
  return <section className="sp-page">
    <header className="page-heading"><div><h1>{t("title", locale)}</h1><p>{t("subtitle", locale)}</p></div><div className="sp-actions">
      <Link className="button button--secondary" to="/service-portfolio/offers">{t("offers", locale)}</Link>
      <Link className="button button--secondary" to="/service-portfolio/quality">{t("quality", locale)}</Link>
      {canManage ? <Link className="button button--secondary" to="/service-portfolio/classification">{t("classification", locale)}</Link> : null}
      {canManage ? <button className="button button--primary" onClick={() => setCreating((v) => !v)}>{t("newItem", locale)}</button> : null}
    </div></header>
    {creating ? <PortfolioItemForm onSaved={(item) => { setCreating(false); navigate(`/service-portfolio/${item.id}`); }} onCancel={() => setCreating(false)} /> : null}
    <form className="panel sp-filter" onSubmit={applyFilters}>
      <label>{t("search", locale)}<input name="q" value={queryDraft} onChange={(e) => setQueryDraft(e.target.value)} maxLength={160} /></label>
      <label>{t("type", locale)}<select name="type" value={type} onChange={(e) => setParams((current) => { const nextParams = new URLSearchParams(current); if (e.target.value) nextParams.set("type", e.target.value); else nextParams.delete("type"); return nextParams; }, { replace: true })}><option value="">{t("all", locale)}</option>{itemTypes.map((value) => <option key={value} value={value}>{displayType(value, locale)}</option>)}</select></label>
      <label>{t("lifecycle", locale)}<select name="lifecycle" value={lifecycle} onChange={(e) => setParams((current) => { const nextParams = new URLSearchParams(current); if (e.target.value) nextParams.set("lifecycle", e.target.value); else nextParams.delete("lifecycle"); return nextParams; }, { replace: true })}><option value="">{t("all", locale)}</option>{["DRAFT", "PUBLISHED", "ACTIVE", "RETIRED"].map((value) => <option key={value} value={value}>{displayLifecycle(value, locale)}</option>)}</select></label>
      <label>{t("environment", locale)}<select name="environment" value={environment} onChange={(e) => setParams((current) => { const nextParams = new URLSearchParams(current); if (e.target.value) nextParams.set("environment", e.target.value); else nextParams.delete("environment"); return nextParams; }, { replace: true })}><option value="">{t("all", locale)}</option>{["DEV", "TEST", "STAGING", "PROD", "ALL"].map((value) => <option key={value}>{value}</option>)}</select></label>
      <button className="button button--primary" type="submit">{t("filter", locale)}</button>
    </form>
    {error ? <PageState title={error} detail={t("loadError", locale)} tone="error" /> : null}
    {loading && items.length === 0 ? <PageState title={t("loading", locale)} /> : null}
    {!loading && !error && items.length === 0 ? <PageState title={t("noItems", locale)} /> : null}
    {items.length ? <div className="panel table-wrap"><table><thead><tr><th>{t("name", locale)}</th><th>{t("type", locale)}</th><th>{t("lifecycle", locale)}</th><th>{t("environment", locale)}</th><th>{t("businessOwner", locale)}</th><th>{t("supportGroup", locale)}</th><th /></tr></thead><tbody>{items.map((item) => <tr key={item.id}>
      <td><strong>{item.name}</strong><small className="sp-subline">{item.service_key}</small></td><td>{displayType(item.type, locale)}</td><td><span className={`sp-status sp-status--${item.lifecycle.toLowerCase()}`}>{displayLifecycle(item.lifecycle, locale)}</span></td><td>{item.environment}</td><td>{item.business_owner_name || "—"}</td><td>{item.support_group_name || "—"}</td><td><Link className="button button--small button--secondary" to={`/service-portfolio/${item.id}`}>{t("open", locale)}</Link></td>
    </tr>)}</tbody></table>{next ? <div className="panel-footer"><button className="button button--secondary" disabled={loading} onClick={() => { const nextUrl = new URL(url, window.location.origin); nextUrl.searchParams.set("before_id", next); load(`${nextUrl.pathname}${nextUrl.search}`, true); }}>{loading ? t("loading", locale) : t("open", locale)} →</button></div> : null}</div> : null}
  </section>;
}

function RelationshipPanel({ item, relations, reload }: { item: PortfolioItem; relations: PortfolioRelation[]; reload: () => void }) {
  const locale = useCMDBLocale();
  const { hasPermission } = useAuth();
  const canManage = hasPermission("service_portfolio.manage");
  const [relationType, setRelationType] = useState<PortfolioRelation["type"]>(item.type === "BUSINESS_APPLICATION" ? "MAPS_TO_CI" : "DEPENDS_ON");
  const [targetKind, setTargetKind] = useState<"items" | "cis">("items");
  const [target, setTarget] = useState<LookupOption | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const ciOnly = relationType === "MAPS_TO_CI";
  useEffect(() => { setTargetKind(ciOnly ? "cis" : "items"); setTarget(null); }, [ciOnly, relationType]);
  async function add(event: FormEvent) {
    event.preventDefault(); if (!target?.id || !target.version) return;
    setBusy(true); setError("");
    try {
      await api(`${base}/items/${item.id}/relations`, { method: "POST", body: jsonBody({
        source_version: item.version, target_item_id: targetKind === "items" ? target.id : "0",
        target_ci_id: targetKind === "cis" ? target.id : "0", target_version: target.version, type: relationType,
      }) });
      setTarget(null); reload();
    } catch (cause) { setError(errorText(cause, t("saveError", locale))); }
    finally { setBusy(false); }
  }
  async function remove(relation: PortfolioRelation) {
    if (!window.confirm(locale === "en" ? "Remove this relationship?" : "Usunąć tę relację?")) return;
    setBusy(true); setError("");
    try {
      await api(`${base}/items/${item.id}/relations/${relation.id}`, { method: "DELETE", body: jsonBody({ source_version: item.version, relation_version: relation.version, target_version: relation.target_version }) });
      reload();
    } catch (cause) { setError(errorText(cause, t("saveError", locale))); }
    finally { setBusy(false); }
  }
  return <section className="panel form-stack"><div className="panel-heading"><h2>{t("relationships", locale)}</h2></div>
    {error ? <PageState title={error} tone="error" /> : null}
    {relations.length === 0 ? <p className="muted">—</p> : <div className="table-wrap"><table><thead><tr><th>{t("relationType", locale)}</th><th>{t("targetService", locale)}</th><th>{t("type", locale)}</th><th /></tr></thead><tbody>{relations.map((relation) => <tr key={relation.id}><td>{relation.type}</td><td>{relation.target_name}</td><td>{relation.target_type || relation.target_ci_class}</td><td>{canManage ? <button type="button" className="button button--small button--danger" disabled={busy} onClick={() => void remove(relation)}>{locale === "en" ? "Remove" : "Usuń"}</button> : null}</td></tr>)}</tbody></table></div>}
    {canManage ? <form className="sp-relation-form" onSubmit={(e) => void add(e)}>
      <label>{t("relationType", locale)}<select value={relationType} onChange={(e) => setRelationType(e.target.value as PortfolioRelation["type"])}>
        {item.type === "SERVICE_INSTANCE" ? <option value="REALIZES">REALIZES</option> : null}
        {(item.type === "SERVICE_INSTANCE" || item.type === "TECHNICAL_SERVICE" || item.type === "BUSINESS_SERVICE") ? <option value="DEPENDS_ON">DEPENDS_ON</option> : null}
        {(item.type === "SERVICE_INSTANCE" || item.type === "TECHNICAL_SERVICE" || item.type === "BUSINESS_SERVICE" || item.type === "BUSINESS_APPLICATION") ? <option value="MAPS_TO_CI">MAPS_TO_CI</option> : null}
      </select></label>
      {ciOnly ? <LookupPicker kind="cis" label={t("targetCI", locale)} value={target} onChange={setTarget} /> : <LookupPicker kind="items" label={t("targetService", locale)} value={target} onChange={setTarget} />}
      <button className="button button--primary" type="submit" disabled={busy || !target}>{busy ? t("saving", locale) : t("addRelation", locale)}</button>
    </form> : null}
  </section>;
}

function ImpactPanel({ impact }: { impact: ImpactResult | null }) {
  const locale = useCMDBLocale();
  if (!impact) return <PageState title={t("loading", locale)} />;
  return <section className="panel form-stack"><div className="panel-heading"><h2>{t("impact", locale)}</h2><small>{formatDateTime(impact.calculated_at, locale)}</small></div>
    {impact.truncated ? <PageState title={t("graphTruncated", locale)} /> : null}
    <p className="muted">{locale === "en" ? `Bounded graph: ${impact.nodes.length}/${impact.max_nodes} nodes, depth ${impact.max_depth}, rule set ${impact.rule_version}.` : `Graf ograniczony: ${impact.nodes.length}/${impact.max_nodes} węzłów, głębokość ${impact.max_depth}, zestaw reguł ${impact.rule_version}.`}</p>
    <div className="sp-impact-columns"><div><h3>{t("impactedServices", locale)}</h3>{impact.impacted_services.length ? <ul>{impact.impacted_services.map((service) => <li key={service.id}><Link to={`/service-portfolio/${service.id.split(":")[1]}`}>{service.name}</Link><small className="sp-subline">{service.path.join(" → ")} · {service.rule}</small></li>)}</ul> : <p className="muted">—</p>}</div><div><h3>{t("impactedOffers", locale)}</h3>{impact.impacted_offers.length ? <ul>{impact.impacted_offers.map((offer) => <li key={offer.id}>{offer.name}<small className="sp-subline">{offer.path.join(" → ")} · {offer.rule}</small></li>)}</ul> : <p className="muted">—</p>}</div></div>
    <details><summary>{locale === "en" ? `Graph nodes (${impact.nodes.length})` : `Węzły grafu (${impact.nodes.length})`}</summary><div className="table-wrap"><table><thead><tr><th>ID</th><th>{t("name", locale)}</th><th>{t("type", locale)}</th><th>{t("availability", locale)}</th><th>{t("rule", locale)}</th></tr></thead><tbody>{impact.nodes.map((node) => <tr key={node.id}><td>{node.id}</td><td>{node.name}</td><td>{node.class ?? node.kind}</td><td>{node.availability ?? "—"}</td><td>{node.availability_rule ?? "—"}</td></tr>)}</tbody></table></div></details>
    {impact.rules_used.length ? <small className="muted">{locale === "en" ? "Rules used: " : "Użyte reguły: "}{impact.rules_used.join(", ")}</small> : null}
  </section>;
}

export function ServicePortfolioDetailPage() {
  const locale = useCMDBLocale();
  const { id = "" } = useParams();
  const { hasPermission } = useAuth();
  const canManage = hasPermission("service_portfolio.manage");
  const canReadIncidents = hasPermission("incident.read") || hasPermission("incident.read_own");
  const canReadRequests = hasPermission("request.read_own") || hasPermission("request.manage") || hasPermission("request.fulfill");
  const [item, setItem] = useState<PortfolioItem | null>(null);
  const [relations, setRelations] = useState<PortfolioRelation[]>([]);
  const [impact, setImpact] = useState<ImpactResult | null>(null);
  const [availability, setAvailability] = useState<Availability | null>(null);
  const [offers, setOffers] = useState<PortfolioOffer[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState(false);
  const [availabilityState, setAvailabilityState] = useState<Availability["state"]>("UNKNOWN");
  const [availabilityNote, setAvailabilityNote] = useState("");
  const [retirement, setRetirement] = useState<RetirementImpact | null>(null);
  const [retireReason, setRetireReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [feedback, setFeedback] = useState("");
  const [related, setRelated] = useState<{ available: boolean; message?: string; items: { id: string; number?: string; title?: string; name?: string; status?: string }[] } | null>(null);
  const [relatedError, setRelatedError] = useState("");
  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true); setError("");
    try {
      const record = await api<PortfolioItem>(`${base}/items/${id}`, { signal });
      setItem(record);
      const calls = await Promise.allSettled([
        api<{ items: PortfolioRelation[] }>(`${base}/items/${id}/relations?limit=100`, { signal }),
        api<ImpactResult>(`${base}/items/${id}/impact`, { signal }),
        api<Availability>(`${base}/availability/ITEM/${id}`, { signal }),
        api<Page<PortfolioOffer>>(`${base}/offers?limit=100`, { signal }),
      ]);
      const [rel, graph, avail, offerPage] = calls;
      if (rel.status === "fulfilled") setRelations(rel.value.items);
      if (graph.status === "fulfilled") setImpact(graph.value);
      if (avail.status === "fulfilled") { setAvailability(avail.value); setAvailabilityState(avail.value.state); setAvailabilityNote(avail.value.note); }
      if (offerPage.status === "fulfilled") setOffers(offerPage.value.items.filter((offer) => offer.parent_service_id === id && offer.lifecycle === "ACTIVE"));
    } catch (cause) { if (!signal?.aborted) setError(errorText(cause, t("loadError", locale))); }
    finally { if (!signal?.aborted) setLoading(false); }
  }, [id, locale]);
  useEffect(() => { const controller = new AbortController(); void load(controller.signal); return () => controller.abort(); }, [load]);
  async function refresh() { await load(); setRetirement(null); }
  async function transition(action: string) {
    if (!item) return; setBusy(true); setFeedback("");
    try {
      const updated = await api<PortfolioItem>(`${base}/items/${id}/lifecycle/${action}`, { method: "POST", body: jsonBody({ version: item.version, reason: action === "retire" ? retireReason : "" }) });
      setItem(updated); setFeedback(locale === "en" ? "Lifecycle updated." : "Zmieniono cykl życia."); await refresh();
    } catch (cause) { setFeedback(errorText(cause, t("saveError", locale))); }
    finally { setBusy(false); }
  }
  async function preflightRetirement() {
    setBusy(true); setFeedback("");
    try { setRetirement(await api<RetirementImpact>(`${base}/items/${id}/retirement-impact`)); }
    catch (cause) { setFeedback(errorText(cause, t("loadError", locale))); }
    finally { setBusy(false); }
  }
  async function saveAvailability(event: FormEvent) {
    event.preventDefault(); if (!item || !availability) return;
    setBusy(true); setFeedback("");
    try {
      const result = await api<Availability>(`${base}/availability`, { method: "POST", body: jsonBody({ node_kind: "ITEM", node_id: id, version: availability.version, state: availabilityState, note: availabilityNote }) });
      setAvailability(result); setFeedback(locale === "en" ? "Availability observation saved." : "Zapisano obserwację dostępności."); await refresh();
    } catch (cause) { setFeedback(errorText(cause, t("saveError", locale))); }
    finally { setBusy(false); }
  }
  async function loadRelated(kind: string) {
    setRelated(null); setRelatedError("");
    try { setRelated(await api<{ available: boolean; message?: string; items: { id: string; number?: string; title?: string; name?: string; status?: string }[] }>(`${base}/items/${id}/records/${kind}?limit=50`)); }
    catch (cause) { setRelatedError(errorText(cause, t("loadError", locale))); }
  }
  if (loading && !item) return <PageState title={t("loading", locale)} />;
  if (error || !item) return <div><PageState title={error || t("loadError", locale)} tone="error" /><button className="button button--secondary" onClick={() => void load()}>{t("retry", locale)}</button></div>;
  return <section className="sp-page">
    <div className="sp-breadcrumb"><Link to="/service-portfolio">{t("services", locale)}</Link><span>/</span><span>{item.name}</span></div>
    <header className="page-heading"><div><h1>{item.name}</h1><p>{item.service_key} · {displayType(item.type, locale)} · {item.environment}</p><span className={`sp-status sp-status--${item.lifecycle.toLowerCase()}`}>{displayLifecycle(item.lifecycle, locale)}</span></div>
      <div className="sp-actions">{canManage && item.lifecycle === "DRAFT" ? <button className="button button--primary" disabled={busy} onClick={() => void transition("publish")}>{t("publish", locale)}</button> : null}{canManage && item.lifecycle === "PUBLISHED" ? <button className="button button--primary" disabled={busy} onClick={() => void transition("activate")}>{t("activate", locale)}</button> : null}{canManage && item.lifecycle === "ACTIVE" ? <><button className="button button--secondary" disabled={busy} onClick={() => setEditing((value) => !value)}>{locale === "en" ? "Edit draft data" : "Edytuj dane"}</button><button className="button button--danger" disabled={busy} onClick={() => void preflightRetirement()}>{t("retire", locale)}</button></> : null}{canManage && item.lifecycle === "DRAFT" ? <button className="button button--secondary" onClick={() => setEditing((value) => !value)}>{locale === "en" ? "Edit" : "Edytuj"}</button> : null}</div>
    </header>
    {feedback ? <PageState title={feedback} tone={feedback.includes("nie ") || feedback.includes("Could not") ? "error" : "success"} /> : null}
    {editing ? <PortfolioItemForm item={item} onCancel={() => setEditing(false)} onSaved={(updated) => { setItem(updated); setEditing(false); void refresh(); }} /> : null}
    <section className="panel form-stack"><div className="panel-heading"><h2>{t("detail", locale)}</h2><small>{locale === "en" ? "Version" : "Wersja"} {item.version}</small></div>
      <dl className="detail-grid sp-details"><div><dt>{t("businessOwner", locale)}</dt><dd>{item.business_owner_name || "—"}</dd></div><div><dt>{t("technicalOwner", locale)}</dt><dd>{item.technical_owner_name || "—"}</dd></div><div><dt>{t("supportGroup", locale)}</dt><dd>{item.support_group_name || "—"}</dd></div><div><dt>{t("criticality", locale)}</dt><dd>{item.criticality}</dd></div><div><dt>{t("location", locale)}</dt><dd>{item.location_name || "—"}</dd></div><div><dt>{t("orgUnit", locale)}</dt><dd>{item.org_unit_name || "—"}</dd></div><div><dt>{t("department", locale)}</dt><dd>{item.department_name || "—"}</dd></div><div><dt>{t("costCenter", locale)}</dt><dd>{item.cost_center_name || "—"}</dd></div><div><dt>{t("backingCI", locale)}</dt><dd>{item.ci_name ? <>{item.ci_name} · {item.ci_class} <Link to={`/cmdb/${item.ci_id}`}>{locale === "en" ? "Open CI" : "Otwórz CI"}</Link> · <Link to={`/service-portfolio/impact/ci/${item.ci_id}`}>{t("impact", locale)}</Link></> : "—"}</dd></div><div><dt>{t("dependencyMode", locale)}</dt><dd>{item.dependency_mode}{item.dependency_mode === "QUORUM" ? ` (${item.dependency_quorum})` : ""}</dd></div></dl>
      {item.description ? <p>{item.description}</p> : null}{item.scope ? <p><strong>{t("scope", locale)}:</strong> {item.scope}</p> : null}
    </section>
    {retirement ? <section className="panel form-stack"><h2>{t("retirementImpact", locale)}</h2><div className="sp-impact-metrics"><span>{t("activeOffers", locale)} <strong>{retirement.active_offers}</strong></span><span>{t("catalogItems", locale)} <strong>{retirement.active_catalog_items}</strong></span><span>{t("dependentItems", locale)} <strong>{retirement.dependent_items}</strong></span><span>{t("openIncidents", locale)} <strong>{retirement.incidents_visible ? retirement.open_incidents : "—"}</strong></span><span>{t("openRequests", locale)} <strong>{retirement.requests_visible ? retirement.open_requests : "—"}</strong></span></div>
      <p>{locale === "en" ? "Retirement withdraws all active offers and prevents new catalog orders. Existing requests keep their history." : "Wycofanie zamknie aktywne oferty i zablokuje nowe zamówienia z katalogu. Historia istniejących wniosków pozostaje."}</p>
      <label>{t("retireReason", locale)}<textarea required maxLength={1000} value={retireReason} onChange={(e) => setRetireReason(e.target.value)} /></label>
      <div className="sp-actions"><button className="button button--secondary" onClick={() => setRetirement(null)}>{t("cancel", locale)}</button><button className="button button--danger" disabled={!retirement.retirement_allowed || !retireReason.trim() || busy} onClick={() => void transition("retire")}>{t("confirmRetire", locale)}</button></div>
    </section> : null}
    <RelationshipPanel item={item} relations={relations} reload={() => void refresh()} />
    <ImpactPanel impact={impact} />
    <section className="panel form-stack"><h2>{t("availability", locale)}: {availability?.state ?? "UNKNOWN"}</h2><p className="muted">{availability?.source ?? "NONE"} {availability?.note ? `· ${availability.note}` : ""}</p>
      {offers.length ? <div><h3>{t("offers", locale)}</h3><ul>{offers.map((offer) => <li key={offer.id}><Link to="/service-portfolio/offers">{offer.name}</Link>{offer.catalog_item_id ? ` · ${offer.catalog_item_title || "catalog"}` : ""}</li>)}</ul></div> : null}
      {canManage ? <form className="sp-relation-form" onSubmit={(event) => void saveAvailability(event)}><label>{t("availability", locale)}<select value={availabilityState} onChange={(e) => setAvailabilityState(e.target.value as Availability["state"])}>{["AVAILABLE", "DEGRADED", "UNAVAILABLE", "UNKNOWN"].map((value) => <option key={value}>{value}</option>)}</select></label><label>{t("availabilityNote", locale)}<input maxLength={500} value={availabilityNote} onChange={(e) => setAvailabilityNote(e.target.value)} /></label><button className="button button--primary" disabled={busy}>{t("setAvailability", locale)}</button></form> : null}
    </section>
    <section className="panel form-stack"><h2>{t("relatedRecords", locale)}</h2><div className="sp-actions">{canReadIncidents ? <button className="button button--secondary" onClick={() => void loadRelated("incidents")}>{t("incidents", locale)}</button> : null}{canReadRequests ? <button className="button button--secondary" onClick={() => void loadRelated("requests")}>{t("requests", locale)}</button> : null}{canReadIncidents ? <button className="button button--secondary" onClick={() => void loadRelated("problems")}>{locale === "en" ? "Problems" : "Problemy"}</button> : null}{hasPermission("change.read") ? <button className="button button--secondary" onClick={() => void loadRelated("changes")}>{locale === "en" ? "Changes" : "Zmiany"}</button> : null}</div>
      {relatedError ? <PageState title={relatedError} tone="error" /> : null}{related && !related.available ? <PageState title={related.message || t("adapterUnavailable", locale)} /> : null}{related?.available ? related.items.length ? <ul>{related.items.map((record) => <li key={record.id}>{record.number || `#${record.id}`} · {record.title || record.name || ""} {record.status ? `· ${record.status}` : ""}</li>)}</ul> : <p className="muted">—</p> : null}
    </section>
  </section>;
}

type OfferDraft = { offer_key: string; name: string; description: string; availability_conditions: string; support_model: string; obligation_summary: string };
const emptyOffer: OfferDraft = { offer_key: "", name: "", description: "", availability_conditions: "", support_model: "STANDARD", obligation_summary: "" };
export function ServicePortfolioOffersPage() {
  const locale = useCMDBLocale();
  const { hasPermission } = useAuth();
  const canManage = hasPermission("service_portfolio.manage");
  const [offers, setOffers] = useState<(PortfolioOffer | PublicOffer)[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [feedback, setFeedback] = useState("");
  const [showForm, setShowForm] = useState(false);
  const [editingOffer, setEditingOffer] = useState<PortfolioOffer | null>(null);
  const [busy, setBusy] = useState(false);
  const [draft, setDraft] = useState<OfferDraft>(emptyOffer);
  const [parent, setParent] = useState<LookupOption | null>(null);
  const [catalog, setCatalog] = useState<LookupOption | null>(null);
  const [sla, setSla] = useState<LookupOption | null>(null);
  const [audience, setAudience] = useState<LookupOption[]>([]);
  const [groupChoice, setGroupChoice] = useState<LookupOption | null>(null);
  const [errorForm, setErrorForm] = useState("");
  const offerRetryKey = useRef("");
  function updateOfferDraft<K extends keyof OfferDraft>(key: K, value: OfferDraft[K]) {
    offerRetryKey.current = "";
    setDraft((current) => ({ ...current, [key]: value }));
  }
  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true); setError("");
    try { const page = await api<Page<PortfolioOffer | PublicOffer>>(`${base}/offers?limit=100`, { signal }); setOffers(page.items); }
    catch (cause) { if (!signal?.aborted) setError(errorText(cause, t("loadError", locale))); }
    finally { if (!signal?.aborted) setLoading(false); }
  }, [locale]);
  useEffect(() => { const controller = new AbortController(); void load(controller.signal); return () => controller.abort(); }, [load]);
  async function submit(event: FormEvent) {
    event.preventDefault(); setBusy(true); setErrorForm("");
    try {
      if (!editingOffer && !offerRetryKey.current) offerRetryKey.current = crypto.randomUUID();
      const result = await api<PortfolioOffer>(editingOffer ? `${base}/offers/${editingOffer.id}` : `${base}/offers`, { method: editingOffer ? "PUT" : "POST", body: jsonBody({
        version: editingOffer?.version ?? "0", offer_key: draft.offer_key, parent_service_id: parent?.id ?? "0", catalog_item_id: catalog?.id ?? "0",
        name: draft.name, description: draft.description, availability_conditions: draft.availability_conditions,
        support_model: draft.support_model, obligation_summary: draft.obligation_summary,
        sla_definition_id: sla?.id ?? "0", audience_group_ids: audience.map((group) => group.id),
      }), headers: editingOffer ? undefined : { "Idempotency-Key": offerRetryKey.current } });
      setFeedback(`${locale === "en" ? (editingOffer ? "Offer updated" : "Draft created") : (editingOffer ? "Zaktualizowano ofertę" : "Utworzono wersję roboczą")}: ${result.name}`); offerRetryKey.current = ""; setDraft(emptyOffer); setParent(null); setCatalog(null); setSla(null); setAudience([]); setShowForm(false); await load();
    } catch (cause) { setErrorForm(errorText(cause, t("saveError", locale))); }
    finally { setBusy(false); }
  }
  async function transition(offer: PortfolioOffer, action: "publish" | "withdraw") {
    setBusy(true); setFeedback("");
    try { await api(`${base}/offers/${offer.id}/lifecycle/${action}`, { method: "POST", body: jsonBody({ version: offer.version }) }); await load(); }
    catch (cause) { setFeedback(errorText(cause, t("saveError", locale))); }
    finally { setBusy(false); }
  }
  function editOffer(offer: PortfolioOffer) {
    offerRetryKey.current = "";
    setEditingOffer(offer);
    setDraft({ offer_key: offer.offer_key, name: offer.name, description: offer.description, availability_conditions: offer.availability_conditions, support_model: offer.support_model, obligation_summary: offer.obligation_summary });
    setParent(selectedOption(offer.parent_service_id, offer.parent_service_name, { version: "0", type: "BUSINESS_SERVICE" }));
    setCatalog(selectedOption(offer.catalog_item_id === "0" ? "" : offer.catalog_item_id, offer.catalog_item_title || `#${offer.catalog_item_id}`));
    setSla(selectedOption(offer.sla_definition_id === "0" ? "" : offer.sla_definition_id, `#${offer.sla_definition_id}`));
    setAudience((offer.audience_group_ids ?? []).map((id) => ({ id, name: `#${id}` })));
    setShowForm(true); setErrorForm("");
  }
  function closeForm() { offerRetryKey.current = ""; setShowForm(false); setEditingOffer(null); setDraft(emptyOffer); setParent(null); setCatalog(null); setSla(null); setAudience([]); setErrorForm(""); }
  return <section className="sp-page"><header className="page-heading"><div><h1>{t("offers", locale)}</h1><p>{t("subtitle", locale)}</p></div><div className="sp-actions"><Link className="button button--secondary" to="/service-portfolio">{t("services", locale)}</Link>{canManage ? <button className="button button--primary" onClick={() => { if (showForm) closeForm(); else { setEditingOffer(null); setDraft(emptyOffer); setParent(null); setCatalog(null); setSla(null); setAudience([]); setShowForm(true); } }}>{t("createOffer", locale)}</button> : null}</div></header>
    {error ? <PageState title={error} tone="error" /> : null}{feedback ? <PageState title={feedback} tone="success" /> : null}
    {showForm && canManage ? <form className="panel form-stack" onSubmit={(e) => void submit(e)}><div className="panel-heading"><h2>{editingOffer ? (locale === "en" ? "Edit service offer" : "Edytuj ofertę usługi") : t("createOffer", locale)}</h2><button type="button" className="button button--secondary" onClick={closeForm}>{t("cancel", locale)}</button></div>{errorForm ? <PageState title={errorForm} tone="error" /> : null}
      <div className="sp-form-grid"><label>{t("offerKey", locale)}<input required minLength={2} maxLength={64} pattern="[A-Za-z0-9][A-Za-z0-9._-]{1,63}" value={draft.offer_key} disabled={Boolean(editingOffer)} onChange={(e) => updateOfferDraft("offer_key", e.target.value.toUpperCase())} /></label><label>{t("offerName", locale)}<input required maxLength={160} value={draft.name} onChange={(e) => updateOfferDraft("name", e.target.value)} /></label><LookupPicker kind="items" endpoint={`${base}/items?type=BUSINESS_SERVICE`} label={t("parentService", locale)} value={parent} onChange={(value) => { offerRetryKey.current = ""; setParent(value); }} /><LookupPicker kind="catalog_items" label={t("catalogItem", locale)} value={catalog} onChange={(value) => { offerRetryKey.current = ""; setCatalog(value); }} /><LookupPicker kind="sla_policies" label={t("slaPolicy", locale)} value={sla} onChange={(value) => { offerRetryKey.current = ""; setSla(value); }} /><label>{t("supportModel", locale)}<select value={draft.support_model} onChange={(e) => updateOfferDraft("support_model", e.target.value)}>{["STANDARD", "EXTENDED", "PREMIUM", "CUSTOM"].map((value) => <option key={value}>{value}</option>)}</select></label></div>
      <label>{t("description", locale)}<textarea rows={3} maxLength={4000} value={draft.description} onChange={(e) => updateOfferDraft("description", e.target.value)} /></label><div className="sp-form-grid"><label>{t("availabilityConditions", locale)}<textarea required maxLength={1000} rows={3} value={draft.availability_conditions} onChange={(e) => updateOfferDraft("availability_conditions", e.target.value)} /></label><label>{t("obligations", locale)}<textarea required maxLength={1000} rows={3} value={draft.obligation_summary} onChange={(e) => updateOfferDraft("obligation_summary", e.target.value)} /></label></div>
      <div className="sp-audience"><LookupPicker kind="groups" label={t("audience", locale)} value={groupChoice} onChange={setGroupChoice} /><button type="button" className="button button--secondary" disabled={!groupChoice || audience.some((group) => group.id === groupChoice.id)} onClick={() => { if (groupChoice && !audience.some((group) => group.id === groupChoice.id)) { offerRetryKey.current = ""; setAudience([...audience, groupChoice]); } setGroupChoice(null); }}>{t("addAudience", locale)}</button>{audience.map((group) => <button type="button" className="sp-chip" key={group.id} onClick={() => { offerRetryKey.current = ""; setAudience(audience.filter((entry) => entry.id !== group.id)); }}>{optionLabel(group)} ×</button>)}<small className="muted">{t("audienceWarning", locale)}</small></div>
      <button className="button button--primary" disabled={busy || !parent}>{busy ? t("saving", locale) : editingOffer ? (locale === "en" ? "Save offer" : "Zapisz ofertę") : t("createOffer", locale)}</button>
    </form> : null}
    {loading ? <PageState title={t("loading", locale)} /> : null}{!loading && offers.length === 0 ? <PageState title={t("noOffers", locale)} /> : null}
    {offers.length ? <div className="sp-offer-grid">{offers.map((offer) => {
      const adminOffer = "parent_service_id" in offer;
      const status = adminOffer ? offer.lifecycle : "ACTIVE";
      return <article className="panel sp-offer-card" key={offer.id}><div className="panel-heading"><div><small>{offer.offer_key}</small><h2>{offer.name}</h2></div><span className={`sp-status sp-status--${status.toLowerCase()}`}>{displayLifecycle(status, locale)}</span></div><p>{offer.description}</p><dl><dt>{t("parentService", locale)}</dt><dd>{adminOffer ? offer.parent_service_name : offer.service_name}</dd><dt>{t("availabilityConditions", locale)}</dt><dd>{offer.availability_conditions}</dd><dt>{t("supportModel", locale)}</dt><dd>{offer.support_model}</dd><dt>{t("obligations", locale)}</dt><dd>{offer.obligation_summary}</dd>{offer.sla_definition_id !== "0" ? <><dt>{t("slaPolicy", locale)}</dt><dd>#{offer.sla_definition_id}</dd></> : null}</dl><div className="sp-actions">{offer.catalog_item_id !== "0" && hasPermission("catalog.read") ? <Link className="button button--primary" to={`/catalog/${offer.catalog_item_id}`}>{t("order", locale)}</Link> : null}{adminOffer && canManage ? <button className="button button--secondary" disabled={busy || offer.lifecycle === "WITHDRAWN"} onClick={() => editOffer(offer)}>{locale === "en" ? "Edit" : "Edytuj"}</button> : null}{adminOffer && canManage && offer.lifecycle === "DRAFT" ? <button className="button button--secondary" disabled={busy} onClick={() => void transition(offer, "publish")}>{t("publishOffer", locale)}</button> : null}{adminOffer && canManage && offer.lifecycle === "ACTIVE" ? <button className="button button--danger" disabled={busy} onClick={() => void transition(offer, "withdraw")}>{t("withdrawOffer", locale)}</button> : null}</div></article>;
    })}</div> : null}
  </section>;
}

type QualityViolation = { rule: string; severity: string; item_id: string; service_key: string; name: string; message: string };
export function ServicePortfolioQualityPage() {
  const locale = useCMDBLocale();
  const { hasPermission } = useAuth();
  const canManage = hasPermission("service_portfolio.manage");
  const [violations, setViolations] = useState<QualityViolation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [refKind, setRefKind] = useState("ORG_UNIT");
  const [refKey, setRefKey] = useState("");
  const [refName, setRefName] = useState("");
  const [feedback, setFeedback] = useState("");
  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true); setError("");
    try { const page = await api<Page<QualityViolation>>(`${base}/quality?limit=100`, { signal }); setViolations(page.items); }
    catch (cause) { if (!signal?.aborted) setError(errorText(cause, t("loadError", locale))); }
    finally { if (!signal?.aborted) setLoading(false); }
  }, [locale]);
  useEffect(() => { const controller = new AbortController(); void load(controller.signal); return () => controller.abort(); }, [load]);
  async function saveRef(event: FormEvent) {
    event.preventDefault(); setFeedback("");
    try { await api(`${base}/org-refs`, { method: "POST", body: jsonBody({ id: "0", kind: refKind, key: refKey, name: refName, active: true, version: "0" }) }); setRefKey(""); setRefName(""); setFeedback(locale === "en" ? "Organization reference added." : "Dodano słownik organizacyjny."); }
    catch (cause) { setFeedback(errorText(cause, t("saveError", locale))); }
  }
  return <section className="sp-page"><header className="page-heading"><div><h1>{t("quality", locale)}</h1><p>{locale === "en" ? "Quality checks run against the current tenant portfolio and preserve data access rules." : "Kontrola jakości obejmuje portfel bieżącej organizacji i respektuje uprawnienia do danych."}</p></div><Link className="button button--secondary" to="/service-portfolio">{t("services", locale)}</Link></header>
    {error ? <PageState title={error} tone="error" /> : null}{loading ? <PageState title={t("loading", locale)} /> : null}{!loading && !error && !violations.length ? <PageState title={t("noViolations", locale)} /> : null}
    {violations.length ? <div className="panel table-wrap"><table><thead><tr><th>{t("severity", locale)}</th><th>{t("rule", locale)}</th><th>{t("item", locale)}</th><th>{t("message", locale)}</th></tr></thead><tbody>{violations.map((violation, index) => <tr key={`${violation.item_id}:${violation.rule}:${index}`}><td><span className={`sp-severity sp-severity--${violation.severity.toLowerCase()}`}>{violation.severity}</span></td><td>{violation.rule}</td><td><Link to={`/service-portfolio/${violation.item_id}`}>{violation.name} · {violation.service_key}</Link></td><td>{violation.message}</td></tr>)}</tbody></table></div> : null}
    {canManage ? <form className="panel sp-ref-form" onSubmit={(e) => void saveRef(e)}><h2>{locale === "en" ? "Organization references" : "Słowniki organizacyjne"}</h2><p className="muted">{locale === "en" ? "These tenant-scoped references can be mapped to services; ITAM locations are selected from the asset location catalog." : "Te wpisy są odrębne dla organizacji. Lokalizacje wybiera się ze słownika ITAM."}</p><label>{t("type", locale)}<select value={refKind} onChange={(e) => setRefKind(e.target.value)}><option value="ORG_UNIT">ORG_UNIT</option><option value="DEPARTMENT">DEPARTMENT</option><option value="COST_CENTER">COST_CENTER</option></select></label><label>{t("key", locale)}<input required minLength={2} maxLength={64} pattern="[A-Za-z0-9][A-Za-z0-9._-]{1,63}" value={refKey} onChange={(e) => setRefKey(e.target.value.toUpperCase())} /></label><label>{t("name", locale)}<input required maxLength={160} value={refName} onChange={(e) => setRefName(e.target.value)} /></label><button className="button button--primary">{locale === "en" ? "Add reference" : "Dodaj wpis"}</button>{feedback ? <PageState title={feedback} tone={feedback.includes("nie ") || feedback.includes("Could not") ? "error" : "success"} /> : null}</form> : null}
  </section>;
}

type ClassificationCandidate = { ci_id: string; name: string; identifier: string; class: "SERVICE" | "APPLICATION"; version: string };
export function ServicePortfolioClassificationPage() {
  const locale = useCMDBLocale();
  const { hasPermission } = useAuth();
  const [items, setItems] = useState<ClassificationCandidate[]>([]);
  const [types, setTypes] = useState<Record<string, string>>({});
  const [keys, setKeys] = useState<Record<string, string>>({});
  const [next, setNext] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [feedback, setFeedback] = useState("");
  const load = useCallback(async (before = "", append = false, signal?: AbortSignal) => {
    setLoading(true); setError("");
    try {
      const query = new URLSearchParams({ limit: "100" }); if (before) query.set("before_id", before);
      const page = await api<Page<ClassificationCandidate>>(`${base}/classification/preview?${query}`, { signal });
      setItems((current) => append ? [...current, ...page.items] : page.items); setNext(page.next_before_id ?? "");
    } catch (cause) { if (!signal?.aborted) setError(errorText(cause, t("loadError", locale))); }
    finally { if (!signal?.aborted) setLoading(false); }
  }, [locale]);
  useEffect(() => { const controller = new AbortController(); void load("", false, controller.signal); return () => controller.abort(); }, [load]);
  async function submit() {
    const mapped = items.filter((item) => types[item.ci_id]);
    if (!mapped.length) return;
    if (mapped.length > 50) { setFeedback(locale === "en" ? "Select at most 50 items per save." : "Jednorazowo można zapisać najwyżej 50 pozycji."); return; }
    setBusy(true); setFeedback("");
    try {
      const result = await api<{ created: PortfolioItem[] }>(`${base}/classification`, { method: "POST", body: jsonBody({ items: mapped.map((item) => ({ ci_id: item.ci_id, ci_version: item.version, type: types[item.ci_id], service_key: keys[item.ci_id] ?? "" })) }) });
      const created = new Set(result.created.map((item) => item.ci_id));
      setItems((current) => current.filter((item) => !created.has(item.ci_id)));
      setFeedback(locale === "en" ? `Created ${result.created.length} draft service entries.` : `Utworzono ${result.created.length} roboczych pozycji portfela.`);
    } catch (cause) { setFeedback(errorText(cause, t("saveError", locale))); }
    finally { setBusy(false); }
  }
  if (!hasPermission("service_portfolio.manage") || (!hasPermission("cmdb.read") && !hasPermission("cmdb.read_own"))) return <PageState title={locale === "en" ? "This view requires portfolio management and CMDB read access." : "Widok wymaga zarządzania portfelem i odczytu CMDB."} tone="error" />;
  return <section className="sp-page"><header className="page-heading"><div><h1>{t("classification", locale)}</h1><p>{t("ciClassification", locale)}</p></div><Link className="button button--secondary" to="/service-portfolio">{t("services", locale)}</Link></header>
    {error ? <PageState title={error} tone="error" /> : null}{feedback ? <PageState title={feedback} tone={feedback.includes("Created") || feedback.includes("Utworzono") ? "success" : "error"} /> : null}{loading ? <PageState title={t("loading", locale)} /> : null}{!loading && !items.length ? <PageState title={locale === "en" ? "No unclassified SERVICE or APPLICATION CIs." : "Brak nieprzypisanych CI klasy SERVICE lub APPLICATION."} /> : null}
    {items.length ? <div className="panel form-stack"><div className="table-wrap"><table><thead><tr><th>CI</th><th>{t("type", locale)}</th><th>{t("key", locale)}</th><th>{t("chooseType", locale)}</th></tr></thead><tbody>{items.map((item) => <tr key={item.ci_id}><td><strong>{item.name}</strong><small className="sp-subline">{item.identifier} · {item.class} · v{item.version}</small></td><td>{item.class}</td><td><input maxLength={64} aria-label={`${t("key", locale)} ${item.name}`} value={keys[item.ci_id] ?? `LEGACY-CI-${item.identifier.replace(/[^A-Za-z0-9._-]/g, "-").toUpperCase()}`.slice(0, 64)} onChange={(e) => setKeys({ ...keys, [item.ci_id]: e.target.value.toUpperCase() })} /></td><td><select value={types[item.ci_id] ?? ""} onChange={(e) => setTypes({ ...types, [item.ci_id]: e.target.value })}><option value="">{t("chooseType", locale)}</option>{(item.class === "APPLICATION" ? ["BUSINESS_APPLICATION", "SERVICE_INSTANCE"] : ["SERVICE_INSTANCE", "TECHNICAL_SERVICE", "BUSINESS_SERVICE"]).map((type) => <option key={type} value={type}>{displayType(type, locale)}</option>)}</select></td></tr>)}</tbody></table></div><div className="sp-actions"><button type="button" className="button button--primary" onClick={() => void submit()} disabled={busy || !items.some((item) => types[item.ci_id])}>{busy ? t("saving", locale) : t("classifySelected", locale)}</button>{next ? <button type="button" className="button button--secondary" disabled={loading} onClick={() => void load(next, true)}>{t("open", locale)} →</button> : null}</div><small className="muted">{locale === "en" ? "Only rows with an explicit type are submitted. A conflict leaves all CI data untouched." : "Wysyłane są tylko pozycje z jawnym typem. Konflikt nie zmienia danych CI."}</small></div> : null}
  </section>;
}

export function ServicePortfolioCIImpactPage() {
  const locale = useCMDBLocale();
  const { ciID = "" } = useParams();
  const [impact, setImpact] = useState<ImpactResult | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    setError(""); setImpact(null);
    void api<ImpactResult>(`${base}/cis/${ciID}/impact`, { signal: controller.signal }).then(setImpact)
      .catch((cause: unknown) => { if (!controller.signal.aborted) setError(errorText(cause, t("loadError", locale))); });
    return () => controller.abort();
  }, [ciID, locale]);
  return <section className="sp-page"><header className="page-heading"><div><h1>{t("impact", locale)}</h1><p>{locale === "en" ? `Impact graph from configuration item ${ciID}.` : `Graf wpływu od elementu konfiguracji ${ciID}.`}</p></div><Link className="button button--secondary" to="/service-portfolio">{t("services", locale)}</Link></header>{error ? <PageState title={error} tone="error" /> : <ImpactPanel impact={impact} />}<CIAvailabilityEditor ciID={ciID} /></section>;
}

function CIAvailabilityEditor({ ciID }: { ciID: string }) {
  const locale = useCMDBLocale();
  const { hasPermission } = useAuth();
  const canManage = hasPermission("service_portfolio.manage");
  const [availability, setAvailability] = useState<Availability | null>(null);
  const [state, setState] = useState<Availability["state"]>("UNKNOWN");
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    void api<Availability>(`${base}/availability/CI/${ciID}`, { signal: controller.signal }).then((value) => { setAvailability(value); setState(value.state); setNote(value.note); })
      .catch((cause: unknown) => { if (!controller.signal.aborted) setError(errorText(cause, t("loadError", locale))); });
    return () => controller.abort();
  }, [ciID, locale]);
  async function submit(event: FormEvent) {
    event.preventDefault(); if (!availability) return;
    setBusy(true); setError("");
    try {
      const result = await api<Availability>(`${base}/availability`, { method: "POST", body: jsonBody({ node_kind: "CI", node_id: ciID, version: availability.version, state, note }) });
      setAvailability(result);
    } catch (cause) { setError(errorText(cause, t("saveError", locale))); }
    finally { setBusy(false); }
  }
  return <section className="panel form-stack"><h2>{t("availability", locale)}: {availability?.state ?? "UNKNOWN"}</h2>{error ? <PageState title={error} tone="error" /> : null}<p className="muted">{availability?.source ?? "NONE"} {availability?.note ? `· ${availability.note}` : ""}</p>{canManage ? <form className="sp-relation-form" onSubmit={(event) => void submit(event)}><label>{t("availability", locale)}<select value={state} onChange={(event) => setState(event.target.value as Availability["state"])}>{["AVAILABLE", "DEGRADED", "UNAVAILABLE", "UNKNOWN"].map((value) => <option key={value}>{value}</option>)}</select></label><label>{t("availabilityNote", locale)}<input maxLength={500} value={note} onChange={(event) => setNote(event.target.value)} /></label><button className="button button--primary" disabled={busy || !availability}>{t("setAvailability", locale)}</button></form> : null}</section>;
}
