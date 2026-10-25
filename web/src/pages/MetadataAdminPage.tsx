import { useEffect, useMemo, useState, type FormEvent } from "react";
import { ApiError, api, jsonBody } from "../api/client";
import { PageState } from "../components/PageState";
import { useCMDBLocale } from "../cmdb/messages";
import { MetadataFields } from "../metadata/MetadataFields";
import { formatDateTime } from "../metadata/format";
import type {
  Configuration,
  Locale,
  MetadataField,
  MetadataHead,
  MetadataImpact,
  MetadataSchema,
  Option,
  RecordType,
} from "../metadata/contracts";

type Version = { version: string; state: string; created_by: string; created_at: string };
type VersionsPage = { items: Version[]; next_before_id?: string };
type MetadataTab = "organization" | "dictionaries" | "incident" | "ci" | "history";
type DictionaryOption = Option;

const dictionaryDefaults: Record<string, DictionaryOption> = {
  "incident.category": { value: "new_category", labels: { pl: "Nowa kategoria", en: "New category" } },
  "incident.subcategory.new_category": { value: "new_subcategory", labels: { pl: "Nowa podkategoria", en: "New subcategory" } },
  "incident.state": { value: "NEW", labels: { pl: "Nowy", en: "New" } },
  "incident.resolution_code": { value: "FIXED", labels: { pl: "Naprawiono", en: "Fixed" } },
  "ci.environment": { value: "DEV", labels: { pl: "Deweloperskie", en: "Development" } },
  "ci.status": { value: "ACTIVE", labels: { pl: "Aktywny", en: "Active" } },
};
const filterableFieldTypes: MetadataField["type"][] = ["TEXT", "CHOICE", "BOOLEAN", "INTEGER", "DECIMAL", "DATE", "DATETIME", "REFERENCE"];
const searchableFieldTypes: MetadataField["type"][] = ["TEXT", "LONG_TEXT", "CHOICE"];

function copy<T>(value: T): T {
  return structuredClone(value);
}

function newField(): MetadataField {
  return {
    key: "new_field",
    labels: { pl: "Nowe pole", en: "New field" },
    type: "TEXT",
    max_length: 160,
    required: false,
    active: true,
  };
}

function text(locale: Locale, pl: string, en: string) {
  return locale === "en" ? en : pl;
}

export function MetadataAdminPage() {
  const locale = useCMDBLocale();
  const [head, setHead] = useState<MetadataHead>();
  const [draft, setDraft] = useState<Configuration>();
  const [versions, setVersions] = useState<Version[]>([]);
  const [tab, setTab] = useState<MetadataTab>("organization");
  const [dictionaryKey, setDictionaryKey] = useState("incident.category");
  const [recordTypeKey, setRecordTypeKey] = useState<"incident" | "ci">("incident");
  const [impact, setImpact] = useState<MetadataImpact>();
  const [compareFrom, setCompareFrom] = useState("");
  const [comparison, setComparison] = useState<string[]>([]);
  const [ruleExpression, setRuleExpression] = useState('{"op":"EQ","field":"category","value":"hardware"}');
  const [ruleValues, setRuleValues] = useState('{"category":"hardware"}');
  const [ruleResult, setRuleResult] = useState<{ valid: boolean; result: boolean }>();
  const [error, setError] = useState<string>();
  const [notice, setNotice] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);

  async function load() {
    setLoading(true);
    setError(undefined);
    try {
      const [nextHead, history] = await Promise.all([
        api<MetadataHead>("/api/v1/metadata/configuration"),
        api<VersionsPage>("/api/v1/metadata/configuration/versions?limit=50"),
      ]);
      setHead(nextHead);
      setDraft(copy(nextHead.draft));
      setVersions(history.items);
      setCompareFrom(nextHead.published_version || history.items[0]?.version || "");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => { void load(); }, []);

  const dirty = useMemo(() => Boolean(head && draft && JSON.stringify(draft) !== JSON.stringify(head.draft)), [head, draft]);
  const dictionaryKeys = useMemo(() => Object.keys(draft?.dictionaries ?? {}).sort(), [draft]);
  const selectedDictionary = draft?.dictionaries[dictionaryKey] ?? [];
  const recordType = draft?.record_types[recordTypeKey];

  function updateConfig(change: (config: Configuration) => void) {
    setDraft((current) => {
      if (!current) return current;
      const next = copy(current);
      change(next);
      return next;
    });
    setImpact(undefined);
    setNotice(undefined);
  }

  function updateRecordType(change: (type: RecordType) => void) {
    updateConfig((config) => {
      const type = config.record_types[recordTypeKey];
      if (type) change(type);
    });
  }

  async function saveDraft(event?: FormEvent) {
    event?.preventDefault();
    if (!head || !draft || busy) return;
    setBusy(true); setError(undefined); setNotice(undefined);
    try {
      const saved = await api<MetadataHead>("/api/v1/metadata/configuration/draft", {
        method: "PUT",
        body: jsonBody({ version: head.version, configuration: draft }),
      });
      setHead(saved); setDraft(copy(saved.draft));
      setNotice(text(locale, "Szkic zapisany. Konfiguracja nie jest jeszcze aktywna.", "Draft saved. The configuration is not active yet."));
      const nextImpact = await api<MetadataImpact>("/api/v1/metadata/configuration/impact");
      setImpact(nextImpact);
      const history = await api<VersionsPage>("/api/v1/metadata/configuration/versions?limit=50");
      setVersions(history.items);
    } catch (cause) {
      setError(cause instanceof ApiError && cause.status === 409
        ? text(locale, "Inny administrator zapisał nowszą wersję. Twój szkic pozostaje na ekranie; odśwież historię przed ponowieniem.", "Another administrator saved a newer version. Your draft is still on screen; refresh before retrying.")
        : cause instanceof Error ? cause.message : String(cause));
    } finally { setBusy(false); }
  }

  async function publish() {
    if (!head || busy || dirty || !head.draft_version) return;
    setBusy(true); setError(undefined); setNotice(undefined);
    try {
      const result = await api<{ head: MetadataHead; impact: MetadataImpact }>("/api/v1/metadata/configuration/publish", {
        method: "POST", body: jsonBody({ version: head.version }),
      });
      setHead(result.head); setDraft(copy(result.head.draft)); setImpact(result.impact);
      window.dispatchEvent(new Event("opensnow:metadata-published"));
      setNotice(text(locale, "Opublikowano konfigurację. Nowe formularze używają tej wersji.", "Configuration published. New forms now use this version."));
      const history = await api<VersionsPage>("/api/v1/metadata/configuration/versions?limit=50");
      setVersions(history.items);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
      try { setImpact(await api<MetadataImpact>("/api/v1/metadata/configuration/impact")); } catch { /* preserve the publication error */ }
    } finally { setBusy(false); }
  }

  async function restore(version: string) {
    if (!head || busy || !version) return;
    setBusy(true); setError(undefined); setNotice(undefined);
    try {
      const restored = await api<MetadataHead>(`/api/v1/metadata/configuration/versions/${version}/restore`, {
        method: "POST", body: jsonBody({ version: head.version }),
      });
      setHead(restored); setDraft(copy(restored.draft));
      setNotice(text(locale, `Wersja ${version} wczytana jako nowy szkic. Opublikuj ją po sprawdzeniu.`, `Version ${version} restored as a new draft. Review it before publishing.`));
      const history = await api<VersionsPage>("/api/v1/metadata/configuration/versions?limit=50");
      setVersions(history.items);
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
    finally { setBusy(false); }
  }

  async function compare() {
    if (!head?.published_version || !compareFrom) return;
    setError(undefined); setComparison([]);
    try {
      const result = await api<{ changed_paths: string[] }>(`/api/v1/metadata/configuration/versions/${compareFrom}/compare/${head.published_version}`);
      setComparison(result.changed_paths);
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  }

  async function testRule(event: FormEvent) {
    event.preventDefault();
    if (!draft) return;
    setRuleResult(undefined); setError(undefined);
    try {
      const expression = JSON.parse(ruleExpression);
      const values = JSON.parse(ruleValues);
      setRuleResult(await api("/api/v1/metadata/configuration/test", {
        method: "POST", body: jsonBody({ type: recordTypeKey, configuration: draft, expression, values }),
      }));
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  }

  if (loading) return <PageState title={text(locale, "Ładowanie konfiguracji…", "Loading configuration…")} />;
  if (!draft || !head) return <section className="page"><PageState title={text(locale, "Nie udało się wczytać konfiguracji", "Could not load configuration")} detail={error} tone="error" /><button className="button button--secondary mt-3" onClick={() => void load()}>{text(locale, "Spróbuj ponownie", "Retry")}</button></section>;

  const tabs: [MetadataTab, string][] = [
    ["organization", text(locale, "Organizacja", "Organization")],
    ["dictionaries", text(locale, "Słowniki", "Dictionaries")],
    ["incident", "Incident"], ["ci", "CI"], ["history", text(locale, "Historia", "History")],
  ];

  return <section className="page">
    <div className="page-heading"><div><p className="eyebrow">Administration</p><h1>{text(locale, "Słownik danych i formularze", "Data dictionary and forms")}</h1><p className="muted">{text(locale, "Wersja konfiguracji", "Configuration version")} {head.version} · {text(locale, "opublikowana", "published")} {head.published_version || "—"}{head.draft_version ? ` · ${text(locale, "szkic", "draft")} ${head.draft_version}` : ""}</p></div><div className="action-row"><button className="button button--secondary" type="button" disabled={busy} onClick={() => void load()}>{text(locale, "Odśwież", "Refresh")}</button><button className="button button--primary" type="button" disabled={busy || !dirty} onClick={() => void saveDraft()}>{busy ? text(locale, "Zapisywanie…", "Saving…") : text(locale, "Zapisz szkic", "Save draft")}</button><button className="button button--primary" type="button" disabled={busy || dirty || !head.draft_version || head.draft_version === head.published_version} onClick={() => void publish()}>{text(locale, "Opublikuj", "Publish")}</button></div></div>
    {error && <PageState title={text(locale, "Operacja nie powiodła się", "Operation failed")} detail={error} tone="error" />}
    {notice && <p className="notice" role="status">{notice}</p>}
    {impact && <ImpactPanel impact={impact} locale={locale} />}
    <nav className="tabs" aria-label={text(locale, "Konfiguracja metadanych", "Metadata configuration")}>
      {tabs.map(([key, label]) => <button key={key} type="button" className={tab === key ? "is-active" : ""} aria-current={tab === key ? "page" : undefined} onClick={() => setTab(key)}>{label}</button>)}
    </nav>
    {tab === "organization" && <OrganizationEditor config={draft} update={updateConfig} locale={locale} />}
    {tab === "dictionaries" && <section className="panel form-grid"><h2>{text(locale, "Słowniki organizacji", "Organization dictionaries")}</h2><label>{text(locale, "Słownik", "Dictionary")}<select value={dictionaryKey} onChange={(event) => setDictionaryKey(event.target.value)}>{dictionaryKeys.map((key) => <option key={key}>{key}</option>)}</select></label><DictionaryEditor key={dictionaryKey} dictionaryKey={dictionaryKey} options={selectedDictionary} locale={locale} onChange={(options) => updateConfig((config) => { config.dictionaries[dictionaryKey] = options; })} onAdd={() => updateConfig((config) => { config.dictionaries[dictionaryKey] = [...(config.dictionaries[dictionaryKey] ?? []), copy(dictionaryDefaults[dictionaryKey] ?? { value: "new_value", labels: { pl: "Nowa wartość", en: "New value" } })]; })} onCreateSubcategory={() => updateConfig((config) => { const key = "incident.subcategory.new_category"; if (!config.dictionaries[key]) config.dictionaries[key] = [copy(dictionaryDefaults[key]!)]; })} /><PriorityMatrixEditor matrix={draft.priority_matrix} locale={locale} onChange={(matrix) => updateConfig((config) => { config.priority_matrix = matrix; })} /></section>}
    {(tab === "incident" || tab === "ci") && <>
      <div className="panel form-grid"><label>{text(locale, "Typ rekordu", "Record type")}<select value={recordTypeKey} onChange={(event) => setRecordTypeKey(event.target.value as "incident" | "ci")}><option value="incident">Incident</option><option value="ci">CI</option></select></label><p className="muted">{text(locale, "Klucze pól pozostają stałe; wycofane wartości pozostają w bazie i historii.", "Field keys stay stable; retired values remain stored and in history.")}</p><button className="button button--secondary w-fit" type="button" onClick={() => updateRecordType((type) => { const field = newField(); let key = field.key; let suffix = 2; while (type.fields.some((item) => item.key === key)) key = `new_field_${suffix++}`; field.key = key; type.fields.push(field); if (!type.form.sections.length) type.form.sections.push({ key: "additional", label: text(locale, "Informacje dodatkowe", "Additional information"), fields: [], columns: 2 }); type.form.sections[0]!.fields.push(key); type.list.columns.push(key); type.list.filters.push(key); })}>{text(locale, "Dodaj pole", "Add field")}</button></div>
      <div className="grid gap-3">{recordType?.fields.map((field) => <FieldEditor key={`${recordTypeKey}:${field.key}`} field={field} recordType={recordType!} locale={locale} canRename={!head.published.record_types[recordTypeKey]?.fields.some((published) => published.key === field.key)} updateType={updateRecordType} rename={(oldKey, newKey) => updateRecordType((type) => renameField(type, oldKey, newKey))} retire={() => updateRecordType((type) => { const current = type.fields.find((candidate) => candidate.key === field.key); if (current) current.active = false; type.form.sections.forEach((section) => { section.fields = section.fields.filter((key) => key !== field.key); }); type.list.columns = type.list.columns.filter((key) => key !== field.key); type.list.filters = type.list.filters.filter((key) => key !== field.key); type.list.search_fields = type.list.search_fields.filter((key) => key !== field.key); })} />)}</div>
      {recordType && <>
        <LayoutEditor recordType={recordType} locale={locale} update={updateRecordType} />
        <FormPreview recordType={recordType} recordTypeKey={recordTypeKey} locale={locale} />
      </>}
      <RuleTester locale={locale} expression={ruleExpression} values={ruleValues} result={ruleResult} setExpression={setRuleExpression} setValues={setRuleValues} submit={testRule} />
    </>}
    {tab === "history" && <section className="grid gap-4"><div className="panel form-grid"><h2>{text(locale, "Porównanie wersji", "Version comparison")}</h2><label>{text(locale, "Wersja źródłowa", "Source version")}<select value={compareFrom} onChange={(event) => setCompareFrom(event.target.value)}>{versions.map((version) => <option key={version.version} value={version.version}>v{version.version} · {version.state}</option>)}</select></label><p className="muted">{text(locale, "Porównanie z aktualnie opublikowaną wersją", "Compared with the currently published version")} {head.published_version}.</p><button className="button button--secondary w-fit" type="button" disabled={!head.published_version || !compareFrom} onClick={() => void compare()}>{text(locale, "Porównaj", "Compare")}</button>{comparison.length > 0 ? <ul>{comparison.map((path) => <li key={path}><code>{path}</code></li>)}</ul> : comparison.length === 0 && !error ? <p className="muted">{text(locale, "Po uruchomieniu porównania pojawią się zmienione ścieżki.", "Changed paths will appear after you run a comparison.")}</p> : null}</div><div className="panel"><h2>{text(locale, "Historia konfiguracji", "Configuration history")}</h2><div className="table-wrap mt-3"><table><thead><tr><th>Version</th><th>State</th><th>{text(locale, "Utworzono", "Created")}</th><th>{text(locale, "Akcje", "Actions")}</th></tr></thead><tbody>{versions.map((version) => <tr key={version.version}><td>{version.version}</td><td>{version.state}</td><td>{formatDateTime(version.created_at, locale)}</td><td><button className="button button--small button--secondary" disabled={busy || dirty} onClick={() => void restore(version.version)}>{text(locale, "Wczytaj jako szkic", "Restore as draft")}</button></td></tr>)}</tbody></table></div></div></section>}
  </section>;
}

function OrganizationEditor({ config, update, locale }: { config: Configuration; update: (change: (config: Configuration) => void) => void; locale: Locale }) {
  const org = config.organization;
  function set<K extends keyof Configuration["organization"]>(key: K, value: Configuration["organization"][K]) { update((next) => { next.organization[key] = value; }); }
  return <section className="panel form-grid"><h2>{text(locale, "Ustawienia organizacji", "Organization settings")}</h2><label>{text(locale, "Nazwa organizacji", "Organization name")}<input maxLength={160} value={org.name} onChange={(event) => set("name", event.target.value)} /></label><label>{text(locale, "Domyślny język", "Default language")}<select value={org.default_locale} onChange={(event) => set("default_locale", event.target.value as Locale)}><option value="pl">Polski</option><option value="en">English</option></select></label><label>{text(locale, "Strefa czasowa", "Time zone")}<input maxLength={100} value={org.time_zone} placeholder="Europe/Warsaw" onChange={(event) => set("time_zone", event.target.value)} /></label><label>{text(locale, "Format daty", "Date format")}<select value={org.date_format} onChange={(event) => set("date_format", event.target.value as typeof org.date_format)}><option>YYYY-MM-DD</option><option>DD.MM.YYYY</option><option>MM/DD/YYYY</option></select></label><label>{text(locale, "Format liczb", "Number format")}<select value={org.number_format} onChange={(event) => set("number_format", event.target.value as typeof org.number_format)}><option>1,234.56</option><option>1.234,56</option></select></label><label>{text(locale, "Domyślna waluta", "Default currency")}<input maxLength={3} pattern="[A-Z]{3}" value={org.currency} onChange={(event) => set("currency", event.target.value.toUpperCase())} /></label><label>{text(locale, "Kolor marki", "Brand color")}<input type="color" value={org.brand_color} onChange={(event) => set("brand_color", event.target.value)} /></label><label>{text(locale, "Logo (HTTPS URL)", "Logo (HTTPS URL)")}<input type="url" pattern="https://.*" maxLength={2048} value={org.logo_url ?? ""} onChange={(event) => set("logo_url", event.target.value)} /></label><p className="muted">{text(locale, "Język użytkownika może nadpisać ustawienie organizacji. Waluta domyślna nie przelicza zapisanych kwot.", "A user's language can override the organization default. The default currency does not convert stored amounts.")}</p></section>;
}

function DictionaryEditor({ dictionaryKey, options, locale, onChange, onAdd, onCreateSubcategory }: { dictionaryKey: string; options: Option[]; locale: Locale; onChange: (options: Option[]) => void; onAdd: () => void; onCreateSubcategory: () => void }) {
  const canRemove = dictionaryKey !== "incident.state";
  return <><div className="table-wrap"><table><thead><tr><th>Key</th><th>Polski</th><th>English</th><th>{text(locale, "Akcje", "Actions")}</th></tr></thead><tbody>{options.map((option, index) => <tr key={`${index}:${option.value}`}><td><input aria-label={`${dictionaryKey} key ${index + 1}`} maxLength={48} value={option.value} onChange={(event) => { const next = copy(options); next[index]!.value = event.target.value.toUpperCase().replace(/[^A-Z0-9_]/g, "_"); onChange(next); }} /></td><td><input aria-label={`${dictionaryKey} label pl ${index + 1}`} maxLength={160} value={option.labels.pl} onChange={(event) => { const next = copy(options); next[index]!.labels.pl = event.target.value; onChange(next); }} /></td><td><input aria-label={`${dictionaryKey} label en ${index + 1}`} maxLength={160} value={option.labels.en} onChange={(event) => { const next = copy(options); next[index]!.labels.en = event.target.value; onChange(next); }} /></td><td><button type="button" className="button button--small button--secondary" disabled={!canRemove} title={!canRemove ? text(locale, "Wartości wymagane przez reguły procesu nie mogą zostać usunięte.", "Values required by lifecycle rules cannot be removed.") : undefined} onClick={() => onChange(options.filter((_, itemIndex) => itemIndex !== index))}>{text(locale, "Usuń", "Remove")}</button></td></tr>)}</tbody></table></div><div className="action-row"><button className="button button--secondary" type="button" onClick={onAdd}>{text(locale, "Dodaj wartość", "Add value")}</button>{dictionaryKey === "incident.category" && <button className="button button--secondary" type="button" onClick={onCreateSubcategory}>{text(locale, "Dodaj słownik podkategorii dla nowej kategorii", "Add a subcategory dictionary for the new category")}</button>}</div></>;
}

function FieldEditor({ field, recordType, locale, canRename, updateType, rename, retire }: { field: MetadataField; recordType: RecordType; locale: Locale; canRename: boolean; updateType: (change: (type: RecordType) => void) => void; rename: (oldKey: string, newKey: string) => void; retire: () => void }) {
  const update = (change: (field: MetadataField) => void) => updateType((type) => { const index = type.fields.findIndex((candidate) => candidate.key === field.key); if (index >= 0) change(type.fields[index]!); });
  const set = <K extends keyof MetadataField>(key: K, value: MetadataField[K]) => update((current) => { current[key] = value; });
  const inColumns = recordType.list.columns.includes(field.key);
  const inFilters = recordType.list.filters.includes(field.key);
  const inSearch = recordType.list.search_fields.includes(field.key);
  function changeType(nextType: MetadataField["type"]) {
    if (!searchableFieldTypes.includes(nextType) && inSearch && recordType.list.search_fields.length <= 1) return;
    update((current) => {
      current.type = nextType;
      current.default = undefined;
      if (nextType !== "TEXT" && nextType !== "LONG_TEXT") delete current.max_length;
      if (nextType !== "DECIMAL") { delete current.precision; delete current.scale; }
      if (nextType !== "DECIMAL" && nextType !== "INTEGER") { delete current.min; delete current.max; }
      if (nextType !== "CHOICE" && nextType !== "MULTI_CHOICE") delete current.options;
      if (nextType !== "REFERENCE") delete current.reference_type;
      if (nextType === "TEXT") current.max_length = 160;
      if (nextType === "LONG_TEXT") current.max_length = 4000;
      if (nextType === "DECIMAL") { current.precision = 10; current.scale = 2; }
      if (nextType === "CHOICE" || nextType === "MULTI_CHOICE") current.options = [];
      if (nextType === "REFERENCE") current.reference_type = "user";
    });
    if (!filterableFieldTypes.includes(nextType)) updateType((type) => { type.list.filters = type.list.filters.filter((key) => key !== field.key); });
    if (!searchableFieldTypes.includes(nextType)) updateType((type) => { type.list.search_fields = type.list.search_fields.filter((key) => key !== field.key); });
  }
  return <details className="panel" open={field.active}><summary className="cursor-pointer font-semibold">{field.labels[locale] || field.key} · <code>{field.key}</code> · {field.type}{!field.active ? ` · ${text(locale, "wycofane", "retired")}` : ""}</summary><div className="form-grid mt-4">
    <label>Stable key<input maxLength={48} disabled={!canRename} title={!canRename ? text(locale, "Klucz opublikowanego pola jest stały.", "A published field key is immutable.") : undefined} value={field.key} onChange={(event) => rename(field.key, event.target.value.toLowerCase().replace(/[^a-z0-9_]/g, "_"))} /></label>
    <label>Polski<input maxLength={160} value={field.labels.pl} onChange={(event) => set("labels", { ...field.labels, pl: event.target.value })} /></label><label>English<input maxLength={160} value={field.labels.en} onChange={(event) => set("labels", { ...field.labels, en: event.target.value })} /></label>
    <label>{text(locale, "Typ danych", "Data type")}<select value={field.type} onChange={(event) => changeType(event.target.value as MetadataField["type"])}>{["TEXT", "LONG_TEXT", "BOOLEAN", "INTEGER", "DECIMAL", "DATE", "DATETIME", "CHOICE", "MULTI_CHOICE", "REFERENCE"].map((type) => <option key={type}>{type}</option>)}</select></label>
    {["TEXT", "LONG_TEXT"].includes(field.type) && <label>{text(locale, "Maksymalna długość", "Maximum length")}<input type="number" min={1} max={field.type === "TEXT" ? 4000 : 100000} value={field.max_length ?? ""} onChange={(event) => set("max_length", Number(event.target.value))} /></label>}
    {field.type === "DECIMAL" && <><label>Precision<input type="number" min={1} max={20} value={field.precision ?? 10} onChange={(event) => set("precision", Number(event.target.value))} /></label><label>Scale<input type="number" min={0} max={6} value={field.scale ?? 2} onChange={(event) => set("scale", Number(event.target.value))} /></label></>}
    {["INTEGER", "DECIMAL"].includes(field.type) && <><label>Min<input type="number" step="any" value={field.min == null ? "" : String(field.min)} onChange={(event) => set("min", event.target.value === "" ? undefined : Number(event.target.value))} /></label><label>Max<input type="number" step="any" value={field.max == null ? "" : String(field.max)} onChange={(event) => set("max", event.target.value === "" ? undefined : Number(event.target.value))} /></label></>}
    {field.type === "REFERENCE" && <label>{text(locale, "Typ referencji", "Reference target")}<select value={field.reference_type ?? "user"} onChange={(event) => set("reference_type", event.target.value as MetadataField["reference_type"])}>{["user", "group", "incident", "request", "ci", "asset"].map((target) => <option key={target}>{target}</option>)}</select></label>}
    <label>{text(locale, "Wartość domyślna (JSON)", "Default value (JSON)")}<input value={field.default === undefined ? "" : JSON.stringify(field.default)} onChange={(event) => { try { set("default", event.target.value ? JSON.parse(event.target.value) : undefined); } catch { /* Server validation reports the last committed value; invalid drafts remain visible in the field editor. */ } }} /></label>
    <label className="flex items-center gap-2"><input type="checkbox" checked={field.required} onChange={(event) => set("required", event.target.checked)} />{text(locale, "Wymagane", "Required")}</label><label className="flex items-center gap-2"><input type="checkbox" checked={Boolean(field.unique)} onChange={(event) => set("unique", event.target.checked)} />Unique</label>
    <label className="flex items-center gap-2"><input type="checkbox" checked={inColumns} onChange={(event) => updateType((type) => { const columns = new Set(type.list.columns); event.target.checked ? columns.add(field.key) : columns.delete(field.key); type.list.columns = [...columns]; })} />{text(locale, "Pokaż w liście", "Show in list")}</label>
    <label className="flex items-center gap-2"><input type="checkbox" checked={inFilters} disabled={!filterableFieldTypes.includes(field.type) || inFilters && recordType.list.filters.length <= 1} onChange={(event) => updateType((type) => { const filters = new Set(type.list.filters); event.target.checked ? filters.add(field.key) : filters.delete(field.key); type.list.filters = [...filters]; })} />{text(locale, "Dostępny jako filtr", "Enable list filter")}</label>
    {searchableFieldTypes.includes(field.type) && <label className="flex items-center gap-2"><input type="checkbox" checked={inSearch} disabled={inSearch && recordType.list.search_fields.length <= 1} onChange={(event) => updateType((type) => { const fields = new Set(type.list.search_fields); event.target.checked ? fields.add(field.key) : fields.delete(field.key); type.list.search_fields = [...fields]; })} />{text(locale, "Przeszukiwany przez q", "Searchable with q")}</label>}
    {["CHOICE", "MULTI_CHOICE"].includes(field.type) && <OptionEditor field={field} locale={locale} update={update} />}
    <RuleEditor title={text(locale, "Reguła widoczności (JSON)", "Visibility rule (JSON")} rule={field.visible_when} locale={locale} onChange={(visible_when) => set("visible_when", visible_when)} /><RuleEditor title={text(locale, "Reguła wymagalności (JSON)", "Required rule (JSON)")} rule={field.required_when} locale={locale} onChange={(required_when) => set("required_when", required_when)} />
    {field.active && <button className="button button--secondary w-fit" type="button" onClick={retire}>{text(locale, "Wycofaj pole", "Retire field")}</button>}
  </div></details>;
}

function renameField(type: RecordType, oldKey: string, newKey: string) {
  if (oldKey === newKey || !newKey || type.fields.some((field) => field.key === newKey)) return;
  const rewriteRule = (rule?: MetadataField["visible_when"]) => {
    if (!rule) return;
    if (rule.field === oldKey) rule.field = newKey;
    rule.children?.forEach(rewriteRule);
  };
  const replace = (keys: string[]) => keys.map((key) => key === oldKey ? newKey : key);
  const field = type.fields.find((candidate) => candidate.key === oldKey);
  if (!field) return;
  field.key = newKey;
  type.fields.forEach((candidate) => { rewriteRule(candidate.visible_when); rewriteRule(candidate.required_when); });
  type.form.sections.forEach((section) => { section.fields = replace(section.fields); });
  if (type.form.role_views) for (const key of Object.keys(type.form.role_views)) type.form.role_views[key] = replace(type.form.role_views[key]!);
  type.list.columns = replace(type.list.columns);
  type.list.search_fields = replace(type.list.search_fields);
  type.list.filters = replace(type.list.filters);
  if (type.list.role_views) for (const key of Object.keys(type.list.role_views)) type.list.role_views[key] = replace(type.list.role_views[key]!);
}

function OptionEditor({ field, locale, update }: { field: MetadataField; locale: Locale; update: (change: (field: MetadataField) => void) => void }) {
  const options = field.options ?? [];
  return <fieldset className="form-grid border-t border-slate-200 pt-3"><legend>{text(locale, "Opcje pola wyboru", "Choice options")}</legend>{options.map((option, index) => <div className="filter-grid" key={`${index}:${option.value}`}><label>Value<input value={option.value} onChange={(event) => update((current) => { const next = [...(current.options ?? [])]; next[index] = { ...next[index]!, value: event.target.value.toLowerCase().replace(/[^a-z0-9_]/g, "_") }; current.options = next; })} /></label><label>PL<input value={option.labels.pl} onChange={(event) => update((current) => { const next = [...(current.options ?? [])]; next[index] = { ...next[index]!, labels: { ...next[index]!.labels, pl: event.target.value } }; current.options = next; })} /></label><label>EN<input value={option.labels.en} onChange={(event) => update((current) => { const next = [...(current.options ?? [])]; next[index] = { ...next[index]!, labels: { ...next[index]!.labels, en: event.target.value } }; current.options = next; })} /></label><button type="button" className="button button--small button--secondary" onClick={() => update((current) => { current.options = (current.options ?? []).filter((_, at) => at !== index); })}>{text(locale, "Usuń", "Remove")}</button></div>)}<button type="button" className="button button--secondary w-fit" onClick={() => update((current) => { current.options = [...(current.options ?? []), { value: `option_${options.length + 1}`, labels: { pl: `Opcja ${options.length + 1}`, en: `Option ${options.length + 1}` } }]; })}>{text(locale, "Dodaj opcję", "Add option")}</button></fieldset>;
}

function RuleEditor({ title, rule, locale, onChange }: { title: string; rule?: MetadataField["visible_when"]; locale: Locale; onChange: (rule: MetadataField["visible_when"]) => void }) {
  const [raw, setRaw] = useState(rule ? JSON.stringify(rule, null, 2) : "");
  const [invalid, setInvalid] = useState(false);
  useEffect(() => { setRaw(rule ? JSON.stringify(rule, null, 2) : ""); }, [rule]);
  return <label>{title}<textarea rows={4} value={raw} onChange={(event) => { setRaw(event.target.value); if (!event.target.value.trim()) { setInvalid(false); onChange(undefined); return; } try { onChange(JSON.parse(event.target.value)); setInvalid(false); } catch { setInvalid(true); } }} />{invalid && <small className="text-red-700">{text(locale, "Wyrażenie musi być poprawnym JSON-em.", "Expression must be valid JSON.")}</small>}</label>;
}

const systemListFields: Record<"incident" | "ci", { key: string; labels: Record<Locale, string> }[]> = {
  incident: [
    { key: "number", labels: { pl: "Numer", en: "Number" } }, { key: "title", labels: { pl: "Tytuł", en: "Title" } },
    { key: "state", labels: { pl: "Stan", en: "State" } }, { key: "priority", labels: { pl: "Priorytet", en: "Priority" } },
    { key: "category", labels: { pl: "Kategoria", en: "Category" } }, { key: "subcategory", labels: { pl: "Podkategoria", en: "Subcategory" } },
    { key: "impact", labels: { pl: "Wpływ", en: "Impact" } }, { key: "urgency", labels: { pl: "Pilność", en: "Urgency" } },
    { key: "affected_user_id", labels: { pl: "Osoba dotknięta", en: "Affected user" } },
  ],
  ci: [
    { key: "identifier", labels: { pl: "Identyfikator", en: "Identifier" } }, { key: "name", labels: { pl: "Nazwa", en: "Name" } },
    { key: "class", labels: { pl: "Klasa", en: "Class" } }, { key: "status", labels: { pl: "Stan", en: "Status" } },
    { key: "environment", labels: { pl: "Środowisko", en: "Environment" } }, { key: "owner_id", labels: { pl: "Właściciel", en: "Owner" } },
    { key: "location", labels: { pl: "Lokalizacja", en: "Location" } },
  ],
};
const systemSearchFields: Record<"incident" | "ci", { key: string; labels: Record<Locale, string> }[]> = {
  incident: [
    { key: "number", labels: { pl: "Numer", en: "Number" } }, { key: "title", labels: { pl: "Tytuł", en: "Title" } },
    { key: "state", labels: { pl: "Stan", en: "State" } }, { key: "category", labels: { pl: "Kategoria", en: "Category" } },
  ],
  ci: [
    { key: "identifier", labels: { pl: "Identyfikator", en: "Identifier" } }, { key: "name", labels: { pl: "Nazwa", en: "Name" } },
    { key: "class", labels: { pl: "Klasa", en: "Class" } }, { key: "status", labels: { pl: "Stan", en: "Status" } },
    { key: "environment", labels: { pl: "Środowisko", en: "Environment" } }, { key: "location", labels: { pl: "Lokalizacja", en: "Location" } },
  ],
};

function LayoutEditor({ recordType, locale, update }: { recordType: RecordType; locale: Locale; update: (change: (type: RecordType) => void) => void }) {
  const systemFields = systemListFields[recordType.key as "incident" | "ci"];
  const selectable = [...systemFields, ...recordType.fields.filter((field) => field.active).map((field) => ({ key: field.key, labels: field.labels }))];
  const formFields = recordType.fields.filter((field) => field.active).map((field) => ({ key: field.key, labels: field.labels }));
  function assignField(field: string, sectionKey: string) {
    update((type) => {
      type.form.sections.forEach((section) => { section.fields = section.fields.filter((key) => key !== field); });
      if (sectionKey) type.form.sections.find((section) => section.key === sectionKey)?.fields.push(field);
    });
  }
  function moveField(field: string, sectionIndex: number, direction: -1 | 1) {
    update((type) => {
      const keys = type.form.sections[sectionIndex]?.fields;
      if (!keys) return;
      const index = keys.indexOf(field);
      const target = index + direction;
      if (index < 0 || target < 0 || target >= keys.length) return;
      [keys[index], keys[target]] = [keys[target]!, keys[index]!];
    });
  }
  function setColumn(field: string, enabled: boolean) {
    update((type) => { if (!enabled && type.list.columns.length <= 1) return; const columns = type.list.columns.filter((key) => key !== field); if (enabled) columns.push(field); type.list.columns = columns; });
  }
  function setSearch(field: string, enabled: boolean) {
    update((type) => { if (!enabled && type.list.search_fields.length <= 1) return; const fields = type.list.search_fields.filter((key) => key !== field); if (enabled) fields.push(field); type.list.search_fields = fields; });
  }
  function setTab(sectionKey: string, enabled: boolean) {
    update((type) => { const tabs = type.form.tabs ?? []; type.form.tabs = enabled ? [...new Set([...tabs, sectionKey])] : tabs.filter((key) => key !== sectionKey); });
  }
  function addSection() {
    update((type) => {
      let suffix = type.form.sections.length + 1;
      let key = `section_${suffix}`;
      while (type.form.sections.some((section) => section.key === key)) key = `section_${++suffix}`;
      type.form.sections.push({ key, label: text(locale, "Nowa sekcja", "New section"), labels: { pl: "Nowa sekcja", en: "New section" }, fields: [], columns: 2 });
    });
  }
  return <section className="panel form-grid"><h2>{text(locale, "Układ formularza i listy", "Form and list layout")}</h2>
    <label>{text(locale, "Domyślne sortowanie", "Default sort")}<select value={recordType.list.default_sort} onChange={(event) => update((type) => { type.list.default_sort = event.target.value; })}><option value="newest">Newest</option><option value="oldest">Oldest</option>{recordType.key === "incident" && <option value="priority">Priority</option>}</select></label>
    <div className="flex flex-wrap items-center justify-between gap-3"><h3>{text(locale, "Sekcje i zakładki formularza", "Form sections and tabs")}</h3><button className="button button--secondary" type="button" onClick={addSection}>{text(locale, "Dodaj sekcję", "Add section")}</button></div>
    {recordType.form.sections.map((section, index) => <div key={section.key} className="panel form-grid"><strong><code>{section.key}</code></strong><div className="filter-grid"><label>Polski<input maxLength={100} value={section.labels?.pl ?? section.label} onChange={(event) => update((type) => { const current = type.form.sections[index]!; const labels = { pl: event.target.value, en: current.labels?.en ?? current.label }; current.labels = labels; current.label = labels.pl; })} /></label><label>English<input maxLength={100} value={section.labels?.en ?? section.label} onChange={(event) => update((type) => { const current = type.form.sections[index]!; const labels = { pl: current.labels?.pl ?? current.label, en: event.target.value }; current.labels = labels; current.label = labels.pl; })} /></label></div><label>{text(locale, "Kolumny", "Columns")}<select value={section.columns} onChange={(event) => update((type) => { type.form.sections[index]!.columns = Number(event.target.value); })}><option value={1}>1</option><option value={2}>2</option><option value={3}>3</option><option value={4}>4</option></select></label><label className="flex items-center gap-2"><input type="checkbox" checked={(recordType.form.tabs ?? []).includes(section.key)} onChange={(event) => setTab(section.key, event.target.checked)} />{text(locale, "Pokaż jako zakładkę", "Show as a tab")}</label><div className="grid gap-2">{formFields.map((field) => { const placed = section.fields.indexOf(field.key); const currentSection = recordType.form.sections.find((candidate) => candidate.fields.includes(field.key))?.key ?? ""; return <div className="filter-grid items-end" key={field.key}><span>{field.labels[locale] || field.key}{placed >= 0 && <span className="muted"> · {placed + 1}</span>}</span><label>{text(locale, "Sekcja pola", "Field section")}<select value={currentSection} onChange={(event) => assignField(field.key, event.target.value)}><option value="">{text(locale, "Bez sekcji", "Unplaced")}</option>{recordType.form.sections.map((candidate) => <option key={candidate.key} value={candidate.key}>{candidate.labels?.[locale] || candidate.label}</option>)}</select></label>{placed >= 0 && <div className="action-row"><button className="button button--small button--secondary" type="button" aria-label={text(locale, `Przesuń ${field.key} w górę`, `Move ${field.key} up`)} disabled={placed === 0} onClick={() => moveField(field.key, index, -1)}>↑</button><button className="button button--small button--secondary" type="button" aria-label={text(locale, `Przesuń ${field.key} w dół`, `Move ${field.key} down`)} disabled={placed === section.fields.length - 1} onClick={() => moveField(field.key, index, 1)}>↓</button></div>}</div>; })}</div><button type="button" className="button button--secondary w-fit" disabled={section.fields.length > 0} onClick={() => update((type) => { type.form.sections.splice(index, 1); type.form.tabs = (type.form.tabs ?? []).filter((key) => key !== section.key); })}>{text(locale, "Usuń pustą sekcję", "Remove empty section")}</button></div>)}
    <div className="form-grid border-t border-slate-200 pt-3"><h3>{text(locale, "Kolumny listy", "List columns")}</h3>{selectable.map((field) => <label className="flex items-center gap-2" key={field.key}><input type="checkbox" checked={recordType.list.columns.includes(field.key)} disabled={recordType.list.columns.includes(field.key) && recordType.list.columns.length <= 1} onChange={(event) => setColumn(field.key, event.target.checked)} />{field.labels[locale] || field.key}</label>)}</div>
    <div className="form-grid border-t border-slate-200 pt-3"><h3>{text(locale, "Pola wyszukiwane przez q", "Fields searched by q")}</h3>{systemSearchFields[recordType.key as "incident" | "ci"].map((field) => <label className="flex items-center gap-2" key={field.key}><input type="checkbox" checked={recordType.list.search_fields.includes(field.key)} disabled={recordType.list.search_fields.includes(field.key) && recordType.list.search_fields.length <= 1} onChange={(event) => setSearch(field.key, event.target.checked)} />{field.labels[locale]}</label>)}<p className="muted">{text(locale, "Pola własne typu tekst/tekst długi/wybór włącza się w edytorze definicji pola.", "Enable custom text, long text, and choice fields in each field editor.")}</p></div>
    <RoleViewEditor title={text(locale, "Widoki formularza według uprawnień", "Form views by effective permission")} views={recordType.form.role_views ?? {}} fields={recordType.fields.filter((field) => field.active).map((field) => ({ key: field.key, label: field.labels[locale] || field.key }))} locale={locale} onChange={(views) => update((type) => { type.form.role_views = views; })} />
    <RoleViewEditor title={text(locale, "Widoki listy według uprawnień", "List views by effective permission")} views={recordType.list.role_views ?? {}} fields={selectable.map((field) => ({ key: field.key, label: field.labels[locale] || field.key }))} locale={locale} onChange={(views) => update((type) => { type.list.role_views = views; })} />
    <p className="muted">{text(locale, "Widok wybiera pola według efektywnego uprawnienia użytkownika. Nie zmienia ACL ani zakresu rekordów dostępnych przez API.", "A view selects fields based on a user's effective permission. It does not change API ACLs or accessible records.")}</p>
  </section>;
}

function RoleViewEditor({ title, views, fields, locale, onChange }: { title: string; views: Record<string, string[]>; fields: { key: string; label: string }[]; locale: Locale; onChange: (views: Record<string, string[]>) => void }) {
  const [permission, setPermission] = useState(Object.keys(views)[0] ?? "");
  const [newPermission, setNewPermission] = useState("");
  const selected = views[permission] ?? [];
  function addView() {
    const key = newPermission.trim();
    if (!/^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$/.test(key)) return;
    onChange({ ...views, [key]: views[key] ?? [] });
    setPermission(key);
    setNewPermission("");
  }
  return <fieldset className="form-grid border-t border-slate-200 pt-3"><legend>{title}</legend>{Object.keys(views).length > 0 && <label>{text(locale, "Edytowany widok", "View to edit")}<select value={Object.hasOwn(views, permission) ? permission : Object.keys(views)[0]} onChange={(event) => setPermission(event.target.value)}>{Object.keys(views).sort().map((key) => <option key={key}>{key}</option>)}</select></label>}<label>{text(locale, "Dodaj klucz uprawnienia", "Add permission key")}<input maxLength={100} placeholder="incident.update" list="metadata-permission-keys" value={newPermission} onChange={(event) => setNewPermission(event.target.value)} /><datalist id="metadata-permission-keys"><option value="incident.read" /><option value="incident.update" /><option value="incident.assign" /><option value="cmdb.read" /><option value="cmdb.manage" /><option value="assets.read" /><option value="request.fulfill" /></datalist></label><div className="action-row"><button className="button button--secondary" type="button" onClick={addView} disabled={!/^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$/.test(newPermission.trim())}>{text(locale, "Dodaj widok", "Add view")}</button>{Object.hasOwn(views, permission) && <button className="button button--secondary" type="button" onClick={() => { const rest = Object.fromEntries(Object.entries(views).filter(([key]) => key !== permission)); onChange(rest); setPermission(Object.keys(rest)[0] ?? ""); }}>{text(locale, "Usuń ten widok", "Remove this view")}</button>}</div>{Object.hasOwn(views, permission) && <div className="grid gap-2">{fields.map((field) => <label key={field.key} className="flex items-center gap-2"><input type="checkbox" checked={selected.includes(field.key)} onChange={(event) => onChange({ ...views, [permission]: event.target.checked ? [...selected, field.key] : selected.filter((key) => key !== field.key) })} />{field.label}</label>)}</div>}<small className="muted">{text(locale, "Wpisz konkretne uprawnienie RBAC. Uprawnienia wynikają z ról i członkostw, a serwer nadal sprawdza dostęp do rekordów.", "Enter an RBAC permission. Permissions come from roles and memberships; the server still checks record access.")}</small></fieldset>;
}

function PriorityMatrixEditor({ matrix, locale, onChange }: { matrix: number[][]; locale: Locale; onChange: (matrix: number[][]) => void }) {
  const set = (row: number, column: number, value: number) => { const next = matrix.map((line) => [...line]); next[row]![column] = value; onChange(next); };
  return <fieldset className="form-grid border-t border-slate-200 pt-3"><legend>{text(locale, "Macierz priorytetu", "Priority matrix")}</legend><p className="muted">{text(locale, "Wiersze to wpływ, kolumny to pilność. Zmiana wpływa na wyliczenie priorytetu nowych zmian.", "Rows are impact and columns are urgency. Changes affect calculated priority on subsequent updates.")}</p><div className="table-wrap"><table><thead><tr><th>{text(locale, "Wpływ / pilność", "Impact / urgency")}</th>{[1, 2, 3].map((n) => <th key={n}>{n}</th>)}</tr></thead><tbody>{[0, 1, 2].map((row) => <tr key={row}><th>{row + 1}</th>{[0, 1, 2].map((column) => <td key={column}><input aria-label={`${text(locale, "Wpływ", "Impact")} ${row + 1}, ${text(locale, "pilność", "urgency")} ${column + 1}`} type="number" min={1} max={5} value={matrix[row]?.[column] ?? 1} onChange={(event) => set(row, column, Number(event.target.value))} /></td>)}</tr>)}</tbody></table></div></fieldset>;
}

function FormPreview({ recordType, recordTypeKey, locale }: { recordType: RecordType; recordTypeKey: "incident" | "ci"; locale: Locale }) {
  const [values, setValues] = useState<Record<string, unknown>>({});
  useEffect(() => setValues({}), [recordTypeKey]);
  const schema: MetadataSchema = { type_key: recordTypeKey, version: "0", fields: recordType.fields, form: recordType.form, list: recordType.list };
  return <section className="panel grid gap-3"><h2>{text(locale, "Podgląd formularza", "Form preview")}</h2><p className="muted">{text(locale, "Podgląd uwzględnia typy pól, reguły prezentacji, sekcje, kolumny i widok bieżącego użytkownika.", "Preview uses field types, display rules, sections, columns, and the current user's view.")}</p><MetadataFields schema={schema} values={values} onChange={setValues} locale={locale} /></section>;
}

function RuleTester({ locale, expression, values, result, setExpression, setValues, submit }: { locale: Locale; expression: string; values: string; result?: { valid: boolean; result: boolean }; setExpression: (value: string) => void; setValues: (value: string) => void; submit: (event: FormEvent) => void }) {
  return <form className="panel form-grid" onSubmit={submit}><h2>{text(locale, "Test reguły", "Rule test")}</h2><label>{text(locale, "Wyrażenie JSON", "Expression JSON")}<textarea rows={5} value={expression} onChange={(event) => setExpression(event.target.value)} /></label><label>{text(locale, "Wartości wejściowe JSON", "Input values JSON")}<textarea rows={5} value={values} onChange={(event) => setValues(event.target.value)} /></label><button className="button button--secondary w-fit" type="submit">{text(locale, "Sprawdź regułę", "Evaluate rule")}</button>{result && <p role="status" className="notice">{result.valid ? text(locale, result.result ? "Reguła zwróciła prawdę." : "Reguła zwróciła fałsz.", result.result ? "Rule returned true." : "Rule returned false.") : text(locale, "Nieprawidłowe dane testowe.", "Invalid rule input.")}</p>}</form>;
}

function ImpactPanel({ impact, locale }: { impact: MetadataImpact; locale: Locale }) {
  return <section className={`panel ${impact.can_publish ? "border-l-4 border-emerald-600" : "border-l-4 border-amber-600"}`}><h2 className="font-semibold">{impact.can_publish ? text(locale, "Wpływ: publikacja jest możliwa", "Impact: publish is allowed") : text(locale, "Wpływ: publikacja jest zablokowana", "Impact: publishing is blocked")}</h2>{impact.changes.length === 0 ? <p className="muted mt-2">{text(locale, "Brak kolizji z zapisanymi rekordami.", "No conflicts with stored records.")}</p> : <ul className="mt-2 grid gap-2">{impact.changes.map((change) => <li key={`${change.key}:${change.kind}`}><strong>{change.key}</strong> · {change.kind} · {text(locale, "rekordów:", "records:")} {change.count} — {change.message}{change.examples?.length ? <div className="muted">{change.examples.join(", ")}</div> : null}</li>)}</ul>}</section>;
}
