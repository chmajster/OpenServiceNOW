import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { api, jsonBody } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { PageState } from "../components/PageState";
import { useIncidentTitleFocus } from "../incidents/hooks";
import { useCMDBLocale } from "../cmdb/messages";
import type { Metadata } from "../cmdb/contracts";

type DataSource = {
  id: string; source_key: string; name: string; source_type: string; owner_id?: string;
  scope_key: string; connection_ref?: string; source_instance_id?: string;
  state: string; priority: number; is_system: boolean; version: string;
};
type IdentityRule = {
  class: string; version: string; match_keys: string[][]; required_attributes: string[];
  normalization: Record<string, string>; parent_dependent: boolean;
  parent_relation_type?: string; parent_role?: string; state: string;
};
type FieldPolicy = {
  class: string; field_key: string; strategy: string; authoritative_source_id?: string;
  allow_manual_override: boolean; version: string;
};
type IREConflict = {
  id: string; source_id: string; run_id?: string; observation_id?: string;
  conflict_type: string; state: string; source_key: string; candidate_ci_ids: string[];
  value_diff: unknown; reference_diff: unknown; resolution_note: string; version: string;
};
type ReconciliationRun = {
  id: string; source_id: string; mode: string; snapshot_scope: string;
  complete_snapshot: boolean; state: string; rule_versions: unknown; version: string;
  counts: Record<string, number>; error_code?: string;
};
type ReconciliationObservation = {
  id: string; row_number: number; state: string; ci_id?: string;
  candidate_ci_ids: string[]; reason_code?: string; reason_message?: string;
  accepted_changes: Record<string, unknown>; rejected_changes: Record<string, string>;
  observation: { source_native_key: string; scope_key: string; values: Record<string, unknown> };
};
type QualityConfig = {
  enabled: boolean; schedule_hours: number; stale_days: number;
  required_fields: Record<string, string[]>; version: string; next_run_at: string;
};
type QualitySummary = { rule_key: string; valid: number; denominator: number; open: number };
type QualityFinding = {
  id: string; class: string; rule_key: string; severity: string; ci_id: string; field_key: string;
  details: unknown; state: string; detected_at: string; checked_at: string;
};
type Provenance = {
  ci_id: string; version: string; last_seen_at?: string;
  items: { field_key: string; source_id: string; source_key: string; source_name: string;
    run_type: string; run_id?: string; rule_version: string; observed_at: string;
    accepted_at: string; override_type?: string; override_reason?: string; override_until?: string; override_allowed: boolean }[];
};
type Page<T> = { items: T[]; next_before_id?: string };
type MergePlan = {
  source_ci_id: string; canonical_ci_id: string; source_version: string;
  canonical_version: string; references: Record<string, number>; blockers: string[];
  unsupported_references: string[]; plan_token: string;
};

const labels = {
  pl: {
    title: "Identyfikacja i uzgadnianie CI (IRE)", subtitle: "Źródła, reguły tożsamości, uzgadnianie danych i jakość CMDB.",
    sources: "Źródła", rules: "Reguły tożsamości", policies: "Uzgadnianie pól", match: "Tester dopasowania",
    runs: "Uzgadnianie danych", conflicts: "Konflikty i scalanie", quality: "Jakość danych",
    source: "Źródło", key: "Klucz", name: "Nazwa", type: "Typ", scope: "Zakres", priority: "Priorytet (niższa liczba wygrywa)",
    connection: "Referencja połączenia", instance: "Identyfikator instancji", state: "Stan", active: "Aktywne", disabled: "Wyłączone",
    save: "Zapisz", create: "Utwórz", edit: "Edytuj", refresh: "Odśwież", busy: "Zapisywanie…", system: "Systemowe",
    class: "Klasa CI", ruleJson: "Konfiguracja reguły (JSON)", policyJson: "Konfiguracja polityki (JSON)",
    observation: "Obserwacja (JSON)", testMatch: "Sprawdź dopasowanie", matchResult: "Wynik dopasowania",
    batch: "Obserwacje partii (JSON)", mode: "Tryb", incremental: "Przyrostowy", full: "Pełny zrzut",
    snapshotScope: "Zakres zrzutu", completeSnapshot: "Zrzut jest kompletny", idempotencyKey: "Klucz idempotencji",
    startDryRun: "Uruchom symulację", apply: "Zastosuj uzgodnienie", cancel: "Anuluj przebieg", rows: "Wiersze przebiegu",
    noRuns: "Brak przebiegów.", noConflicts: "Brak otwartych konfliktów.", resolve: "Rozstrzygnij", dismiss: "Odrzuć konflikt",
    candidate: "Kanoniczny CI", note: "Uzasadnienie decyzji", planMerge: "Przygotuj plan scalania", sourceCI: "CI do scalenia",
    canonicalCI: "Kanoniczny CI", mergeReason: "Powód scalenia", merge: "Wykonaj scalenie", qualityConfig: "Konfiguracja kontroli jakości",
    scheduleHours: "Częstotliwość (godz.)", staleDays: "Próg nieaktualności (dni)", requiredFields: "Wymagane pola według klasy (JSON)",
    enabled: "Kontrola aktywna", runQuality: "Uruchom kontrolę teraz", metrics: "Wyniki według reguły",
    valid: "Poprawne", denominator: "Zakres oceny", open: "Otwarte problemy", findings: "Otwarte wyniki",
    noFindings: "Nie znaleziono otwartych problemów jakościowych.", reloadError: "Nie udało się pobrać danych IRE.",
    operationError: "Operacja nie powiodła się", saved: "Zapisano.", runQueued: "Kontrola jakości dodana do kolejki.",
    qualityReadOnly: "Możesz przeglądać wyniki jakości, ale konfiguracja i uruchamianie wymagają uprawnienia do zarządzania.",
    findingsNeedCIRead: "Podgląd wyników CI wymaga uprawnienia cmdb.read lub cmdb.read_own.",
    noAccess: "Wymagane uprawnienie do IRE lub odczytu jakości CMDB.", noData: "Brak danych.", details: "Szczegóły",
    lastSeen: "Ostatnio widziany", provenance: "Pochodzenie pól", field: "Pole", sourceName: "Źródło danych",
    observed: "Zaobserwowano", accepted: "Przyjęto", override: "Ręczne nadpisanie", overrideType: "Typ nadpisania",
    permanent: "Stałe", temporary: "Tymczasowe", expires: "Wygasa", reason: "Powód", setOverride: "Ustaw nadpisanie",
    clearOverride: "Usuń nadpisanie", selectField: "Wybierz pole", noProvenance: "Brak zapisanej proweniencji pól.",
    openCI: "Otwórz CI", pending: "Oczekuje", dryRun: "Symulacja", completed: "Zakończony", partial: "Częściowy",
    blockers: "Blokady", references: "Referencje", unsupported: "Nieobsługiwane referencje", mergeReady: "Plan nie wykrył blokad.",
    ruleVersion: "Wersja reguły", candidates: "Kandydaci", result: "Stan", sourceNativeKey: "Klucz źródłowy",
    runState: "Stan przebiegu", qualityNext: "Następna kontrola", policyStrategy: "Strategia",
  },
  en: {
    title: "CI Identification and Reconciliation (IRE)", subtitle: "Sources, identity rules, data reconciliation and CMDB quality.",
    sources: "Sources", rules: "Identity rules", policies: "Field reconciliation", match: "Match tester",
    runs: "Reconciliation runs", conflicts: "Conflicts and merges", quality: "Data quality",
    source: "Source", key: "Key", name: "Name", type: "Type", scope: "Scope", priority: "Priority (lower wins)",
    connection: "Connection reference", instance: "Source instance ID", state: "State", active: "Active", disabled: "Disabled",
    save: "Save", create: "Create", edit: "Edit", refresh: "Refresh", busy: "Saving…", system: "System",
    class: "CI class", ruleJson: "Rule configuration (JSON)", policyJson: "Policy configuration (JSON)",
    observation: "Observation (JSON)", testMatch: "Test match", matchResult: "Match result",
    batch: "Batch observations (JSON)", mode: "Mode", incremental: "Incremental", full: "Full snapshot",
    snapshotScope: "Snapshot scope", completeSnapshot: "Snapshot is complete", idempotencyKey: "Idempotency key",
    startDryRun: "Start dry run", apply: "Apply reconciliation", cancel: "Cancel run", rows: "Run rows",
    noRuns: "No runs.", noConflicts: "No open conflicts.", resolve: "Resolve", dismiss: "Dismiss conflict",
    candidate: "Canonical CI", note: "Decision reason", planMerge: "Build merge plan", sourceCI: "CI to merge",
    canonicalCI: "Canonical CI", mergeReason: "Merge reason", merge: "Apply merge", qualityConfig: "Quality check configuration",
    scheduleHours: "Interval (hours)", staleDays: "Stale threshold (days)", requiredFields: "Required fields by class (JSON)",
    enabled: "Checks enabled", runQuality: "Run checks now", metrics: "Results by rule",
    valid: "Valid", denominator: "Evaluation scope", open: "Open issues", findings: "Open findings",
    noFindings: "No open quality findings.", reloadError: "Could not load IRE data.",
    operationError: "Operation failed", saved: "Saved.", runQueued: "Quality check queued.",
    qualityReadOnly: "You can view quality results. Configuration and manual runs require management access.",
    findingsNeedCIRead: "Viewing CI findings requires cmdb.read or cmdb.read_own permission.",
    noAccess: "IRE management or CMDB quality read permission is required.", noData: "No data.", details: "Details",
    lastSeen: "Last seen", provenance: "Field provenance", field: "Field", sourceName: "Data source",
    observed: "Observed", accepted: "Accepted", override: "Manual override", overrideType: "Override type",
    permanent: "Permanent", temporary: "Temporary", expires: "Expires", reason: "Reason", setOverride: "Set override",
    clearOverride: "Clear override", selectField: "Select field", noProvenance: "No field provenance recorded.",
    openCI: "Open CI", pending: "Pending", dryRun: "Dry run", completed: "Completed", partial: "Partial",
    blockers: "Blockers", references: "References", unsupported: "Unsupported references", mergeReady: "No blockers were found.",
    ruleVersion: "Rule version", candidates: "Candidates", result: "Result", sourceNativeKey: "Source key",
    runState: "Run state", qualityNext: "Next quality check", policyStrategy: "Strategy",
  },
} as const;

const qualityFilterLabels = {
  pl: { owner: "Właściciel (ID)", rule: "Reguła", all: "Wszystkie", open: "Otwarte", resolved: "Rozwiązane", severity: "Waga", filter: "Filtruj", loadMore: "Pokaż więcej", loading: "Ładowanie…", empty: "Brak wyników dla wybranych filtrów." },
  en: { owner: "Owner (ID)", rule: "Rule", all: "All", open: "Open", resolved: "Resolved", severity: "Severity", filter: "Filter", loadMore: "Load more", loading: "Loading…", empty: "No findings match these filters." },
} as const;

function jsonValue<T>(value: T) { return JSON.stringify(value, null, 2); }
function message(error: unknown) { return error instanceof Error ? error.message : String(error); }
function nowForInput() { return new Date(Date.now() + 2 * 60 * 60 * 1000).toISOString().slice(0, 16); }

const emptySource: Omit<DataSource, "id" | "version" | "is_system"> & { version: string; is_system: boolean } = {
  source_key: "", name: "", source_type: "CSV", scope_key: "", connection_ref: "",
  source_instance_id: "", state: "ACTIVE", priority: 100, version: "0", is_system: false,
};

export function CMDBIREPage() {
  const locale = useCMDBLocale();
  const t = labels[locale];
  const ft = qualityFilterLabels[locale];
  const title = useIncidentTitleFocus();
  const { hasPermission } = useAuth();
  const canManage = hasPermission("cmdb.ire.manage") && hasPermission("cmdb.manage") && hasPermission("cmdb.read");
  const canReadQuality = hasPermission("cmdb.quality.read") || hasPermission("cmdb.quality.manage") || hasPermission("cmdb.ire.manage");
  const canReadCIQuality = hasPermission("cmdb.read") || hasPermission("cmdb.read_own");
  const canManageQuality = (hasPermission("cmdb.quality.manage") || hasPermission("cmdb.ire.manage")) && hasPermission("cmdb.manage") && hasPermission("cmdb.read");
  const [sources, setSources] = useState<DataSource[]>([]);
  const [rules, setRules] = useState<IdentityRule[]>([]);
  const [policies, setPolicies] = useState<FieldPolicy[]>([]);
  const [runs, setRuns] = useState<ReconciliationRun[]>([]);
  const [conflicts, setConflicts] = useState<IREConflict[]>([]);
  const [config, setConfig] = useState<QualityConfig>();
  const [summary, setSummary] = useState<QualitySummary[]>([]);
  const [findings, setFindings] = useState<QualityFinding[]>([]);
  const [findingPageNext, setFindingPageNext] = useState("");
  const findingPageNextRef = useRef("");
  const findingsRequestRef = useRef(0);
  const [findingsLoading, setFindingsLoading] = useState(false);
  const [classes, setClasses] = useState<Metadata["classes"]>([]);
  const [findingFilterDraft, setFindingFilterDraft] = useState({ class: "", rule: "", state: "OPEN", owner: "" });
  const [findingFilters, setFindingFilters] = useState({ class: "", rule: "", state: "OPEN", owner: "" });
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [tab, setTab] = useState("sources");
  const currentTab = canManage ? tab : "quality";
  const [sourceDraft, setSourceDraft] = useState<typeof emptySource>(emptySource);
  const [editingSource, setEditingSource] = useState("");
  const [ruleClass, setRuleClass] = useState("APPLICATION");
  const [ruleJSON, setRuleJSON] = useState("");
  const [policyJSON, setPolicyJSON] = useState("{\n  \"class\": \"*\",\n  \"field_key\": \"name\",\n  \"strategy\": \"PRIORITY\",\n  \"allow_manual_override\": false,\n  \"version\": \"0\"\n}");
  const [matchSource, setMatchSource] = useState("");
  const [matchJSON, setMatchJSON] = useState("");
  const [matchResult, setMatchResult] = useState<unknown>();
  const [runSource, setRunSource] = useState("");
  const [runMode, setRunMode] = useState("INCREMENTAL");
  const [runScope, setRunScope] = useState("");
  const [completeSnapshot, setCompleteSnapshot] = useState(false);
  const [idempotencyKey, setIdempotencyKey] = useState("");
  const [batchJSON, setBatchJSON] = useState("[]");
  const [selectedRun, setSelectedRun] = useState("");
  const [runRows, setRunRows] = useState<ReconciliationObservation[]>([]);
  const [conflictCI, setConflictCI] = useState<Record<string, string>>({});
  const [conflictNotes, setConflictNotes] = useState<Record<string, string>>({});
  const [mergeSource, setMergeSource] = useState("");
  const [mergeCanonical, setMergeCanonical] = useState("");
  const [mergePlan, setMergePlan] = useState<MergePlan>();
  const [mergeReason, setMergeReason] = useState("");
  const [qualityFieldsJSON, setQualityFieldsJSON] = useState("");

  const loadFindings = useCallback(async (append = false) => {
    if (!canReadCIQuality) { findingPageNextRef.current = ""; setFindings([]); setFindingPageNext(""); return; }
    const requestID = ++findingsRequestRef.current;
    setFindingsLoading(true);
    try {
      const query = new URLSearchParams({ limit: "100" });
      if (findingFilters.state) query.set("state", findingFilters.state);
      if (findingFilters.class) query.set("class", findingFilters.class);
      if (findingFilters.rule) query.set("rule", findingFilters.rule);
      if (findingFilters.owner && hasPermission("cmdb.read")) query.set("owner_id", findingFilters.owner);
      if (append && findingPageNextRef.current) query.set("before_id", findingPageNextRef.current);
      const page = await api<Page<QualityFinding>>(`/api/v1/cmdb/ire/quality/findings?${query}`);
      if (requestID !== findingsRequestRef.current) return;
      setFindings((current) => append ? [...current, ...page.items] : page.items);
      findingPageNextRef.current = page.next_before_id ?? "";
      setFindingPageNext(page.next_before_id ?? "");
    } catch (cause) { if (requestID === findingsRequestRef.current) setError(message(cause)); }
    finally { if (requestID === findingsRequestRef.current) setFindingsLoading(false); }
  }, [canReadCIQuality, findingFilters, hasPermission]);

  useEffect(() => { void loadFindings(); }, [loadFindings]);

  const reload = useCallback(async () => {
    setError("");
    try {
      if (canManage) {
        const [sourcePage, identityRules, fieldPolicies, runPage, conflictPage] = await Promise.all([
          api<Page<DataSource>>("/api/v1/cmdb/ire/sources?limit=100"),
          api<IdentityRule[]>("/api/v1/cmdb/ire/rules"),
          api<FieldPolicy[]>("/api/v1/cmdb/ire/policies"),
          api<Page<ReconciliationRun>>("/api/v1/cmdb/ire/runs?limit=100"),
          api<Page<IREConflict>>("/api/v1/cmdb/ire/conflicts?state=OPEN&limit=100"),
        ]);
        setSources(sourcePage.items); setRules(identityRules); setPolicies(fieldPolicies);
        setRuns(runPage.items); setConflicts(conflictPage.items);
        if (!matchSource && sourcePage.items[0]) setMatchSource(sourcePage.items[0].id);
        if (!runSource && sourcePage.items.find((item) => item.source_type !== "MANUAL")) setRunSource(sourcePage.items.find((item) => item.source_type !== "MANUAL")!.id);
      }
      if (canReadQuality) {
        const [qualityConfig, metrics] = await Promise.all([
          api<QualityConfig>("/api/v1/cmdb/ire/quality/config"),
          api<QualitySummary[]>("/api/v1/cmdb/ire/quality/summary"),
        ]);
        setConfig(qualityConfig); setSummary(metrics);
        setQualityFieldsJSON(jsonValue(qualityConfig.required_fields));
        if (canReadCIQuality) {
          const cmdbMetadata = await api<Metadata>("/api/v1/cmdb/classes");
          setClasses(cmdbMetadata.classes);
        }
      }
    } catch (cause) {
      setError(message(cause));
    }
  }, [canManage, canReadCIQuality, canReadQuality, matchSource, runSource]);

  useEffect(() => { void reload(); }, [reload]);

  async function action<T>(work: () => Promise<T>, success?: string): Promise<T | undefined> {
    setBusy(true); setError(""); setNotice("");
    try {
      const result = await work();
      if (success) setNotice(success);
      await reload();
      if (canReadCIQuality) await loadFindings();
      return result;
    } catch (cause) {
      setError(message(cause));
      return undefined;
    } finally { setBusy(false); }
  }

  function selectRule(classKey: string) {
    setRuleClass(classKey);
    const current = rules.find((item) => item.class === classKey);
    setRuleJSON(jsonValue(current ? {
      match_keys: current.match_keys, required_attributes: current.required_attributes,
      normalization: current.normalization, parent_dependent: current.parent_dependent,
      parent_relation_type: current.parent_relation_type ?? "", parent_role: current.parent_role ?? "TARGET",
    } : { match_keys: [["identifier"]], required_attributes: [], normalization: {}, parent_dependent: false }));
  }

  useEffect(() => {
    if (rules.length && !ruleJSON) selectRule(ruleClass);
    // Initial content follows the first loaded rule set.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rules, ruleClass, ruleJSON]);

  async function saveSource(event: FormEvent) {
    event.preventDefault();
    await action(async () => {
      const body = {
        source_key: sourceDraft.source_key, name: sourceDraft.name, source_type: sourceDraft.source_type,
        owner_id: sourceDraft.owner_id || undefined, scope_key: sourceDraft.scope_key,
        connection_ref: sourceDraft.connection_ref || undefined, source_instance_id: sourceDraft.source_instance_id || undefined,
        state: sourceDraft.state, priority: Number(sourceDraft.priority), version: sourceDraft.version,
      };
      const path = editingSource ? `/api/v1/cmdb/ire/sources/${editingSource}` : "/api/v1/cmdb/ire/sources";
      const saved = await api<DataSource>(path, { method: editingSource ? "PUT" : "POST", body: jsonBody(body) });
      setSourceDraft({ ...emptySource }); setEditingSource("");
      return saved;
    }, t.saved);
  }

  async function saveRule(event: FormEvent) {
    event.preventDefault();
    await action(async () => {
      const definition = JSON.parse(ruleJSON) as Omit<IdentityRule, "class" | "version" | "state">;
      const version = rules.find((item) => item.class === ruleClass)?.version ?? "0";
      return api(`/api/v1/cmdb/ire/rules/${encodeURIComponent(ruleClass)}`, {
        method: "PUT", body: jsonBody({ class: ruleClass, expected_version: version, ...definition }),
      });
    }, t.saved);
  }

  async function savePolicy(event: FormEvent) {
    event.preventDefault();
    await action(() => api("/api/v1/cmdb/ire/policies", { method: "POST", body: jsonBody(JSON.parse(policyJSON)) }), t.saved);
  }

  async function testMatch(event: FormEvent) {
    event.preventDefault();
    const result = await action(() => api("/api/v1/cmdb/ire/match", {
      method: "POST", body: jsonBody({ source_id: matchSource, observation: JSON.parse(matchJSON) }),
    }));
    if (result !== undefined) setMatchResult(result);
  }

  async function startRun(event: FormEvent) {
    event.preventDefault();
    await action(async () => {
      const observations = JSON.parse(batchJSON);
      if (!Array.isArray(observations) || observations.length === 0) throw new Error("Podaj niepustą tablicę obserwacji." );
      const created = await api<ReconciliationRun>("/api/v1/cmdb/ire/runs", {
        method: "POST", body: jsonBody({ source_id: runSource, idempotency_key: idempotencyKey.trim(), mode: runMode,
          snapshot_scope: runScope, complete_snapshot: completeSnapshot, observations }),
      });
      setSelectedRun(created.id); setIdempotencyKey("");
      return created;
    });
  }

  async function loadRunRows(runID: string) {
    setSelectedRun(runID); setError("");
    try {
      const page = await api<{ items: ReconciliationObservation[] }>(`/api/v1/cmdb/ire/runs/${runID}/rows?limit=100`);
      setRunRows(page.items);
    } catch (cause) { setError(message(cause)); }
  }

  async function runAction(run: ReconciliationRun, kind: "apply" | "cancel") {
    await action(() => api(`/api/v1/cmdb/ire/runs/${run.id}/${kind}`, { method: "POST", body: jsonBody({ version: run.version }) }));
    if (selectedRun === run.id) void loadRunRows(run.id);
  }

  async function resolveConflict(conflict: IREConflict, kind: "resolve" | "dismiss") {
    const note = conflictNotes[conflict.id]?.trim() ?? "";
    const ciID = conflictCI[conflict.id] || conflict.candidate_ci_ids[0] || "";
    await action(() => api(`/api/v1/cmdb/ire/conflicts/${conflict.id}/${kind}`, {
      method: "POST", body: jsonBody({ version: conflict.version, ci_id: kind === "resolve" ? ciID : undefined, note }),
    }));
  }

  async function buildMergePlan(event: FormEvent) {
    event.preventDefault();
    const plan = await action(() => api<MergePlan>("/api/v1/cmdb/ire/merges/plan", {
      method: "POST", body: jsonBody({ source_ci_id: mergeSource, canonical_ci_id: mergeCanonical }),
    }));
    if (plan) setMergePlan(plan);
  }

  async function applyMerge() {
    if (!mergePlan) return;
    await action(() => api("/api/v1/cmdb/ire/merges/apply", {
      method: "POST", body: jsonBody({ source_ci_id: mergePlan.source_ci_id, canonical_ci_id: mergePlan.canonical_ci_id,
        source_version: mergePlan.source_version, canonical_version: mergePlan.canonical_version,
        plan_token: mergePlan.plan_token, reason: mergeReason.trim() }),
    }), t.saved);
    setMergePlan(undefined);
  }

  async function saveQualityConfig(event: FormEvent) {
    event.preventDefault();
    if (!config) return;
    await action(async () => {
      const updated = await api<QualityConfig>("/api/v1/cmdb/ire/quality/config", {
        method: "PUT", body: jsonBody({ ...config, required_fields: JSON.parse(qualityFieldsJSON),
          schedule_hours: Number(config.schedule_hours), stale_days: Number(config.stale_days) }),
      });
      setConfig(updated); return updated;
    }, t.saved);
  }

  async function runQuality() {
    const random = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`;
    await action(() => api("/api/v1/cmdb/ire/quality/run", { method: "POST", headers: { "Idempotency-Key": `quality-${random}` }, body: jsonBody({}) }), t.runQueued);
  }

  if (!canManage && !canReadQuality) return <PageState title={t.noAccess} tone="error" />;

  return <section className="page">
    <div className="page-heading"><div><p className="eyebrow"><Link to="/cmdb">CMDB</Link></p><h1 ref={title} tabIndex={-1}>{t.title}</h1><p className="muted">{t.subtitle}</p></div>
      <button className="button button--secondary" onClick={() => void reload()} disabled={busy}>{t.refresh}</button></div>
    {error && <PageState title={t.operationError} detail={error} tone="error" />}
    {notice && <PageState title={notice} tone="success" />}
    {!canManage && canReadQuality && <p className="notice">{t.qualityReadOnly}</p>}
    <div className="cmdb-tabs" role="tablist" aria-label={t.title}>
      {(canManage ? ["sources", "rules", "policies", "match", "runs", "conflicts", "quality"] : ["quality"]).map((key) =>
        <button key={key} type="button" role="tab" aria-selected={currentTab === key} onClick={() => setTab(key)}>{t[key as keyof typeof t]}</button>)}
    </div>

    {canManage && currentTab === "sources" && <div className="grid gap-4 xl:grid-cols-2">
      <section className="panel"><h2>{editingSource ? t.edit : t.create} — {t.source}</h2>
        <form className="form-grid mt-4" onSubmit={(event) => void saveSource(event)}>
          <label>{t.key}<input required maxLength={80} pattern="[a-z][a-z0-9_.-]+" value={sourceDraft.source_key} disabled={!!editingSource && sources.find((item) => item.id === editingSource)?.is_system || busy} onChange={(e) => setSourceDraft({ ...sourceDraft, source_key: e.target.value })} /></label>
          <label>{t.name}<input required maxLength={160} value={sourceDraft.name} disabled={busy} onChange={(e) => setSourceDraft({ ...sourceDraft, name: e.target.value })} /></label>
          <label>{t.type}<select value={sourceDraft.source_type} disabled={!!editingSource && sources.find((item) => item.id === editingSource)?.is_system || busy} onChange={(e) => setSourceDraft({ ...sourceDraft, source_type: e.target.value })}><option>CSV</option><option>DISCOVERY</option><option>INTEGRATION</option></select></label>
          <label>{t.scope}<input maxLength={255} value={sourceDraft.scope_key} disabled={busy} onChange={(e) => setSourceDraft({ ...sourceDraft, scope_key: e.target.value })} /></label>
          <label>{t.priority}<input type="number" min={1} max={65535} required value={sourceDraft.priority} disabled={busy} onChange={(e) => setSourceDraft({ ...sourceDraft, priority: Number(e.target.value) })} /></label>
          <label>{t.state}<select value={sourceDraft.state} disabled={!!editingSource && sources.find((item) => item.id === editingSource)?.is_system || busy} onChange={(e) => setSourceDraft({ ...sourceDraft, state: e.target.value })}><option value="ACTIVE">{t.active}</option><option value="DISABLED">{t.disabled}</option></select></label>
          <label>{t.connection}<input maxLength={200} value={sourceDraft.connection_ref ?? ""} disabled={busy} onChange={(e) => setSourceDraft({ ...sourceDraft, connection_ref: e.target.value })} /></label>
          <label>{t.instance}<input maxLength={255} value={sourceDraft.source_instance_id ?? ""} disabled={busy} onChange={(e) => setSourceDraft({ ...sourceDraft, source_instance_id: e.target.value })} /></label>
          <div className="action-row"><button className="button" disabled={busy}>{busy ? t.busy : t.save}</button><button className="button button--secondary" type="button" disabled={busy} onClick={() => { setEditingSource(""); setSourceDraft({ ...emptySource }); }}>{t.cancel}</button></div>
        </form>
      </section>
      <section className="panel"><h2>{t.sources}</h2><div className="table-wrap mt-3"><table><thead><tr><th>{t.source}</th><th>{t.type}</th><th>{t.scope}</th><th>{t.priority}</th><th>{t.state}</th><th /></tr></thead><tbody>
        {sources.map((source) => <tr key={source.id}><td>{source.name}<small className="block text-slate-500">{source.source_key}{source.is_system ? ` · ${t.system}` : ""}</small></td><td>{source.source_type}</td><td>{source.scope_key || "—"}</td><td>{source.priority}</td><td>{source.state}</td><td>{!source.is_system && <button className="button button--secondary" onClick={() => { setEditingSource(source.id); setSourceDraft({ ...source, version: source.version, is_system: false }); }}>{t.edit}</button>}</td></tr>)}
        {!sources.length && <tr><td colSpan={6}>{t.noData}</td></tr>}
      </tbody></table></div></section>
    </div>}

    {canManage && currentTab === "rules" && <div className="grid gap-4 xl:grid-cols-2">
      <section className="panel"><h2>{t.rules}</h2><label className="mt-3 block">{t.class}<select value={ruleClass} onChange={(event) => selectRule(event.target.value)}>{rules.map((rule) => <option key={rule.class}>{rule.class}</option>)}</select></label>
        <form className="mt-3" onSubmit={(event) => void saveRule(event)}><label>{t.ruleJson}<textarea rows={16} value={ruleJSON} onChange={(event) => setRuleJSON(event.target.value)} spellCheck={false} /></label><button className="button mt-3" disabled={busy}>{t.save}</button></form></section>
      <section className="panel"><h2>{t.rules}</h2><div className="table-wrap mt-3"><table><thead><tr><th>{t.class}</th><th>{t.ruleVersion}</th><th>{t.state}</th><th>{t.details}</th></tr></thead><tbody>{rules.map((rule) => <tr key={rule.class}><td>{rule.class}</td><td>{rule.version}</td><td>{rule.state}</td><td><code>{rule.match_keys.map((keys) => keys.join(" + ")).join(" | ")}</code>{rule.parent_dependent && <small className="block">{rule.parent_relation_type} / {rule.parent_role}</small>}</td></tr>)}</tbody></table></div></section>
    </div>}

    {canManage && currentTab === "policies" && <div className="grid gap-4 xl:grid-cols-2">
      <section className="panel"><h2>{t.policies}</h2><form className="mt-3" onSubmit={(event) => void savePolicy(event)}><label>{t.policyJson}<textarea rows={12} value={policyJSON} onChange={(event) => setPolicyJSON(event.target.value)} spellCheck={false} /></label><button className="button mt-3" disabled={busy}>{t.save}</button></form><p className="muted mt-3">AUTHORITATIVE, PRIORITY, FRESHEST, FILL_EMPTY</p></section>
      <section className="panel"><h2>{t.policies}</h2><div className="table-wrap mt-3"><table><thead><tr><th>{t.class}</th><th>{t.field}</th><th>{t.policyStrategy}</th><th>{t.source}</th><th>{t.override}</th></tr></thead><tbody>{policies.map((policy) => <tr key={`${policy.class}:${policy.field_key}`}><td>{policy.class}</td><td>{policy.field_key}</td><td>{policy.strategy}</td><td>{sources.find((source) => source.id === policy.authoritative_source_id)?.name ?? policy.authoritative_source_id ?? "—"}</td><td>{policy.allow_manual_override ? "✓" : "—"}</td></tr>)}</tbody></table>{!policies.length && <p className="muted p-3">{t.noData}</p>}</div></section>
    </div>}

    {canManage && currentTab === "match" && <section className="panel"><h2>{t.match}</h2><form className="form-grid mt-4" onSubmit={(event) => void testMatch(event)}>
      <label>{t.source}<select value={matchSource} onChange={(event) => setMatchSource(event.target.value)}>{sources.map((source) => <option key={source.id} value={source.id}>{source.name} ({source.source_key})</option>)}</select></label>
      <label className="xl:col-span-2">{t.observation}<textarea rows={12} required value={matchJSON} onChange={(event) => setMatchJSON(event.target.value)} spellCheck={false} /></label>
      <button className="button" disabled={busy || !matchSource}>{t.testMatch}</button></form>
      {matchResult !== undefined && <section className="mt-4"><h3>{t.matchResult}</h3><pre className="overflow-auto rounded bg-slate-50 p-3">{jsonValue(matchResult)}</pre></section>}
    </section>}

    {canManage && currentTab === "runs" && <div className="grid gap-4 xl:grid-cols-2">
      <section className="panel"><h2>{t.startDryRun}</h2><form className="form-grid mt-4" onSubmit={(event) => void startRun(event)}>
        <label>{t.source}<select required value={runSource} onChange={(event) => setRunSource(event.target.value)}>{sources.filter((source) => source.source_type !== "MANUAL").map((source) => <option key={source.id} value={source.id}>{source.name}</option>)}</select></label>
        <label>{t.mode}<select value={runMode} onChange={(event) => setRunMode(event.target.value)}><option value="INCREMENTAL">{t.incremental}</option><option value="FULL">{t.full}</option></select></label>
        {runMode === "FULL" && <><label>{t.snapshotScope}<input required maxLength={255} value={runScope} onChange={(event) => setRunScope(event.target.value)} /></label><label className="flex items-center gap-2"><input type="checkbox" checked={completeSnapshot} onChange={(event) => setCompleteSnapshot(event.target.checked)} />{t.completeSnapshot}</label></>}
        <label>{t.idempotencyKey}<input required minLength={8} maxLength={128} value={idempotencyKey} onChange={(event) => setIdempotencyKey(event.target.value)} /></label>
        <label className="xl:col-span-2">{t.batch}<textarea rows={14} required value={batchJSON} onChange={(event) => setBatchJSON(event.target.value)} spellCheck={false} /></label>
        <button className="button" disabled={busy || !runSource}>{t.startDryRun}</button>
      </form></section>
      <section className="panel"><h2>{t.runs}</h2><div className="table-wrap mt-3"><table><thead><tr><th>ID</th><th>{t.mode}</th><th>{t.runState}</th><th>{t.details}</th><th /></tr></thead><tbody>{runs.map((run) => <tr key={run.id}><td>{run.id}</td><td>{run.mode}{run.complete_snapshot ? " · complete" : ""}</td><td>{run.state}</td><td>{Object.entries(run.counts ?? {}).map(([key, count]) => `${key}: ${count}`).join(" · ") || "—"}{run.error_code && <small className="block">{run.error_code}</small>}</td><td className="whitespace-nowrap"><button className="button button--secondary" onClick={() => void loadRunRows(run.id)}>{t.rows}</button>{(run.state === "DRY_RUN" || run.state === "PARTIAL") && <button className="button ml-2" disabled={busy} onClick={() => void runAction(run, "apply")}>{t.apply}</button>}{["QUEUED_DRY_RUN", "DRY_RUN", "QUEUED_APPLY"].includes(run.state) && <button className="button button--secondary ml-2" disabled={busy} onClick={() => void runAction(run, "cancel")}>{t.cancel}</button>}</td></tr>)}{!runs.length && <tr><td colSpan={5}>{t.noRuns}</td></tr>}</tbody></table></div>
        {selectedRun && <section className="mt-4"><h3>{t.rows} — {selectedRun}</h3><div className="table-wrap"><table><thead><tr><th>#</th><th>{t.sourceNativeKey}</th><th>{t.result}</th><th>CI</th><th>{t.candidates}</th><th>{t.details}</th></tr></thead><tbody>{runRows.map((row) => <tr key={row.id}><td>{row.row_number}</td><td>{row.observation.source_native_key}</td><td>{row.state}</td><td>{row.ci_id ? <Link to={`/cmdb/${row.ci_id}`}>{row.ci_id}</Link> : "—"}</td><td>{row.candidate_ci_ids.map((id) => <Link className="mr-2" key={id} to={`/cmdb/${id}`}>{id}</Link>)}</td><td>{row.reason_code && <><strong>{row.reason_code}</strong>: </>}{row.reason_message}<details><summary>{t.details}</summary><pre>{jsonValue({ accepted: row.accepted_changes, rejected: row.rejected_changes })}</pre></details></td></tr>)}</tbody></table></div></section>}
      </section>
    </div>}

    {canManage && currentTab === "conflicts" && <div className="grid gap-4 xl:grid-cols-2">
      <section className="panel"><h2>{t.conflicts}</h2>{conflicts.length === 0 && <p className="muted mt-3">{t.noConflicts}</p>}{conflicts.map((conflict) => <article className="border-b border-slate-200 py-4" key={conflict.id}>
        <div className="flex flex-wrap items-start justify-between gap-3"><div><h3>#{conflict.id} · {conflict.conflict_type}</h3><p className="muted">{conflict.source_key}</p>{conflict.candidate_ci_ids.map((ci) => <Link key={ci} className="mr-3" to={`/cmdb/${ci}`}>{t.openCI} {ci}</Link>)}</div><details><summary>{t.details}</summary><pre className="max-w-xl overflow-auto">{jsonValue({ values: conflict.value_diff, references: conflict.reference_diff })}</pre></details></div>
        <div className="form-grid mt-3"><label>{t.candidate}<input type="number" min="1" step="1" list={`ire-candidates-${conflict.id}`} value={conflictCI[conflict.id] || (conflict.candidate_ci_ids.length === 1 ? conflict.candidate_ci_ids[0] : "")} onChange={(event) => setConflictCI({ ...conflictCI, [conflict.id]: event.target.value })} /><datalist id={`ire-candidates-${conflict.id}`}>{conflict.candidate_ci_ids.map((ci) => <option key={ci} value={ci} />)}</datalist></label><label>{t.note}<input minLength={3} maxLength={1000} value={conflictNotes[conflict.id] ?? ""} onChange={(event) => setConflictNotes({ ...conflictNotes, [conflict.id]: event.target.value })} /></label><div className="action-row"><button className="button" disabled={busy || !(conflictCI[conflict.id]?.trim() || conflict.candidate_ci_ids[0]) || (conflictNotes[conflict.id] ?? "").trim().length < 3} onClick={() => void resolveConflict(conflict, "resolve")}>{t.resolve}</button><button className="button button--secondary" disabled={busy || (conflictNotes[conflict.id] ?? "").trim().length < 3} onClick={() => void resolveConflict(conflict, "dismiss")}>{t.dismiss}</button></div></div>
      </article>)}</section>
      <section className="panel"><h2>{t.planMerge}</h2><form className="form-grid mt-4" onSubmit={(event) => void buildMergePlan(event)}><label>{t.sourceCI}<input inputMode="numeric" required value={mergeSource} onChange={(event) => { setMergeSource(event.target.value); setMergePlan(undefined); }} /></label><label>{t.canonicalCI}<input inputMode="numeric" required value={mergeCanonical} onChange={(event) => { setMergeCanonical(event.target.value); setMergePlan(undefined); }} /></label><button className="button">{t.planMerge}</button></form>
        {mergePlan && <div className="mt-4"><p>{mergePlan.blockers.length ? <strong>{t.blockers}: {mergePlan.blockers.join(", ")}</strong> : t.mergeReady}</p><p>{t.references}: {Object.entries(mergePlan.references).map(([key, value]) => `${key}: ${value}`).join(" · ")}</p>{mergePlan.unsupported_references.length > 0 && <p>{t.unsupported}: {mergePlan.unsupported_references.join(", ")}</p>}
          <label>{t.mergeReason}<textarea minLength={5} maxLength={1000} value={mergeReason} onChange={(event) => setMergeReason(event.target.value)} /></label><button className="button mt-3" disabled={busy || !!mergePlan.blockers.length || mergeReason.trim().length < 5} onClick={() => void applyMerge()}>{t.merge}</button></div>}
      </section>
    </div>}

    {canReadQuality && currentTab === "quality" && <div className="grid gap-4 xl:grid-cols-2">
      <section className="panel"><h2>{t.metrics}</h2><div className="table-wrap mt-3"><table><thead><tr><th>{t.policyStrategy}</th><th>{t.valid}</th><th>{t.denominator}</th><th>{t.open}</th></tr></thead><tbody>{summary.map((item) => <tr key={item.rule_key}><td>{item.rule_key}</td><td>{item.valid}</td><td>{item.denominator}</td><td>{item.open}</td></tr>)}{!summary.length && <tr><td colSpan={4}>{t.noData}</td></tr>}</tbody></table></div>
        {canManageQuality && <button className="button mt-3" disabled={busy} onClick={() => void runQuality()}>{t.runQuality}</button>}
      </section>
      <section className="panel"><h2>{t.qualityConfig}</h2>{config && <form className="form-grid mt-4" onSubmit={(event) => void saveQualityConfig(event)}>
        <label className="flex items-center gap-2"><input type="checkbox" checked={config.enabled} disabled={!canManageQuality || busy} onChange={(event) => setConfig({ ...config, enabled: event.target.checked })} />{t.enabled}</label>
        <label>{t.scheduleHours}<input type="number" min={1} max={720} value={config.schedule_hours} disabled={!canManageQuality || busy} onChange={(event) => setConfig({ ...config, schedule_hours: Number(event.target.value) })} /></label>
        <label>{t.staleDays}<input type="number" min={1} max={3650} value={config.stale_days} disabled={!canManageQuality || busy} onChange={(event) => setConfig({ ...config, stale_days: Number(event.target.value) })} /></label>
        <label className="xl:col-span-2">{t.requiredFields}<textarea rows={12} value={qualityFieldsJSON} disabled={!canManageQuality || busy} onChange={(event) => setQualityFieldsJSON(event.target.value)} spellCheck={false} /></label>
        <p className="muted xl:col-span-2">{t.qualityNext}: {config.next_run_at ? new Date(config.next_run_at).toLocaleString(locale === "en" ? "en-GB" : "pl-PL") : "—"}</p>
        {canManageQuality && <button className="button" disabled={busy}>{t.save}</button>}
      </form>}</section>
      <section className="panel xl:col-span-2"><h2>{t.findings}</h2>{!canReadCIQuality ? <p className="muted mt-3">{t.findingsNeedCIRead}</p> : <>
        <form className="form-grid mt-3" onSubmit={(event) => { event.preventDefault(); setFindingFilters({ ...findingFilterDraft }); }}>
          <label>{t.class}<select value={findingFilterDraft.class} onChange={(event) => setFindingFilterDraft({ ...findingFilterDraft, class: event.target.value })}><option value="">{ft.all}</option>{classes.map((item) => <option key={item.key} value={item.key}>{item.label || item.key}</option>)}</select></label>
          <label>{ft.rule}<select value={findingFilterDraft.rule} onChange={(event) => setFindingFilterDraft({ ...findingFilterDraft, rule: event.target.value })}><option value="">{ft.all}</option>{summary.map((item) => <option key={item.rule_key} value={item.rule_key}>{item.rule_key}</option>)}</select></label>
          <label>{t.state}<select value={findingFilterDraft.state} onChange={(event) => setFindingFilterDraft({ ...findingFilterDraft, state: event.target.value })}><option value="OPEN">{ft.open}</option><option value="RESOLVED">{ft.resolved}</option><option value="">{ft.all}</option></select></label>
          {hasPermission("cmdb.read") && <label>{ft.owner}<input type="number" min="1" step="1" value={findingFilterDraft.owner} onChange={(event) => setFindingFilterDraft({ ...findingFilterDraft, owner: event.target.value })} /></label>}
          <div className="action-row"><button className="button" disabled={findingsLoading}>{ft.filter}</button><button type="button" className="button button--secondary" disabled={findingsLoading} onClick={() => void loadFindings()}>{findingsLoading ? ft.loading : t.refresh}</button></div>
        </form>
        <div className="table-wrap mt-3"><table><thead><tr><th>{t.class}</th><th>CI</th><th>{ft.rule}</th><th>{t.field}</th><th>{t.state}</th><th>{ft.severity}</th><th>{t.details}</th><th>{t.observed}</th></tr></thead><tbody>{findings.map((finding) => <tr key={finding.id}><td>{finding.class}</td><td><Link to={"/cmdb/" + finding.ci_id}>{finding.ci_id}</Link></td><td>{finding.rule_key}</td><td>{finding.field_key}</td><td>{finding.state}</td><td>{finding.severity}</td><td><pre className="max-w-lg overflow-auto whitespace-pre-wrap">{jsonValue(finding.details)}</pre></td><td>{finding.detected_at ? new Date(finding.detected_at).toLocaleString(locale === "en" ? "en-GB" : "pl-PL") : "?"}</td></tr>)}{!findings.length && <tr><td colSpan={8}>{findingsLoading ? ft.loading : ft.empty}</td></tr>}</tbody></table></div>
        {findingPageNext && <button className="button button--secondary mt-3" disabled={findingsLoading} onClick={() => void loadFindings(true)}>{findingsLoading ? ft.loading : ft.loadMore}</button>}
      </>}</section>
    </div>}
  </section>;
}

export function CIProvenancePanel({ ciID, version, canManage, onVersionChange }: { ciID: string; version: string; canManage: boolean; onVersionChange: (version: string) => void }) {
  const locale = useCMDBLocale();
  const t = labels[locale];
  const [data, setData] = useState<Provenance>();
  const [field, setField] = useState("");
  const [kind, setKind] = useState("PERMANENT");
  const [reason, setReason] = useState("");
  const [expires, setExpires] = useState(nowForInput());
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let active = true;
    void api<Provenance>(`/api/v1/cmdb/ire/cis/${ciID}/provenance`).then((value) => { if (active) setData(value); }).catch((cause) => { if (active) setError(message(cause)); });
    return () => { active = false; };
  }, [ciID]);
  async function saveOverride(event: FormEvent) {
    event.preventDefault(); setBusy(true); setError(""); setNotice("");
    try {
      const updated = await api<Provenance>(`/api/v1/cmdb/ire/cis/${ciID}/overrides`, { method: "POST", body: jsonBody({ version,
        field_key: field, override_type: kind, reason: reason.trim(), expires_at: kind === "TEMPORARY" ? new Date(expires).toISOString() : undefined }) });
      onVersionChange(updated.version); setData(updated); setNotice(t.saved); setReason("");
    } catch (cause) { setError(message(cause)); } finally { setBusy(false); }
  }
  async function clearOverride(fieldKey: string) {
    setBusy(true); setError(""); setNotice("");
    try {
      const updated = await api<Provenance>(`/api/v1/cmdb/ire/cis/${ciID}/overrides/${encodeURIComponent(fieldKey)}/clear`, { method: "POST", body: jsonBody({ version, reason: reason.trim() }) });
      onVersionChange(updated.version); setData(updated); setNotice(t.saved);
    } catch (cause) { setError(message(cause)); } finally { setBusy(false); }
  }
  return <div className="grid gap-4 xl:grid-cols-2">
    <section className="panel"><h2>{t.provenance}</h2><p className="muted">{t.lastSeen}: {data?.last_seen_at ? new Date(data.last_seen_at).toLocaleString(locale === "en" ? "en-GB" : "pl-PL") : "—"}</p>
      {error && <p className="notice notice--error" role="alert">{error}</p>}{notice && <p className="notice" role="status">{notice}</p>}
      {!data?.items.length && <p className="muted">{t.noProvenance}</p>}
      {data?.items.map((entry) => <article key={entry.field_key} className="border-b border-slate-200 py-3"><h3>{entry.field_key}</h3><p>{t.sourceName}: {entry.source_name} ({entry.source_key}) · {entry.run_type}{entry.run_id ? ` #${entry.run_id}` : ""} · {t.ruleVersion}: {entry.rule_version}</p><p className="muted">{t.observed}: {new Date(entry.observed_at).toLocaleString(locale === "en" ? "en-GB" : "pl-PL")} · {t.accepted}: {new Date(entry.accepted_at).toLocaleString(locale === "en" ? "en-GB" : "pl-PL")}</p>
        {entry.override_type && <p>{t.override}: {entry.override_type === "TEMPORARY" ? t.temporary : t.permanent} · {entry.override_reason}{entry.override_until ? ` · ${t.expires}: ${new Date(entry.override_until).toLocaleString()}` : ""}</p>}
        {canManage && entry.override_type && entry.override_allowed && <button className="button button--secondary" disabled={busy || reason.trim().length < 3} onClick={() => void clearOverride(entry.field_key)}>{t.clearOverride}</button>}</article>)}
    </section>
    {canManage && <section className="panel"><h2>{t.setOverride}</h2><form className="form-grid mt-3" onSubmit={(event) => void saveOverride(event)}>
      <label>{t.selectField}<select required value={field} onChange={(event) => setField(event.target.value)}><option value="">—</option>{data?.items.filter((entry) => entry.override_allowed).map((entry) => <option key={entry.field_key} value={entry.field_key}>{entry.field_key}</option>)}</select></label>
      <label>{t.overrideType}<select value={kind} onChange={(event) => setKind(event.target.value)}><option value="PERMANENT">{t.permanent}</option><option value="TEMPORARY">{t.temporary}</option></select></label>
      {kind === "TEMPORARY" && <label>{t.expires}<input required type="datetime-local" value={expires} onChange={(event) => setExpires(event.target.value)} /></label>}
      <label className="xl:col-span-2">{t.reason}<textarea required minLength={3} maxLength={500} value={reason} onChange={(event) => setReason(event.target.value)} /></label>
      <button className="button" disabled={busy || !field || reason.trim().length < 3}>{t.setOverride}</button>
    </form><p className="muted mt-3">Nadpisanie obejmuje aktualną wartość pola; zmiana samej wartości jest dostępna przez edycję CI.</p></section>}
  </div>;
}
