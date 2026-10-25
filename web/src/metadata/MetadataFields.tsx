import { useEffect, useId, useState } from "react";
import { api } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type { Expression, Locale, MetadataField, MetadataSchema, ReferencePage } from "./contracts";
import { evaluateRule } from "./rules";

function fieldLabel(field: MetadataField, locale: Locale) {
  return field.labels[locale] || field.labels.pl || field.key;
}

function labelByKey(field: MetadataField, locale: Locale) {
  return field.description?.[locale] || field.description?.pl || undefined;
}

function ReferenceField({ field, value, locale, disabled, required, onChange }: {
  field: MetadataField;
  value: unknown;
  locale: Locale;
  disabled: boolean;
  required: boolean;
  onChange: (value: string | undefined) => void;
}) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<ReferencePage["items"]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string>();
  const [open, setOpen] = useState(false);
  const current = value === undefined || value === null || value === "" ? "" : String(value);
  useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      setLoading(true);
      setError(undefined);
      const params = new URLSearchParams({ q: query, limit: "25" });
      void api<ReferencePage>(`/api/v1/metadata/references/${field.reference_type}?${params}`, { signal: controller.signal })
        .then((page) => { if (!controller.signal.aborted) setResults(page.items); })
        .catch((cause) => { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : (locale === "en" ? "Reference search failed." : "Nie udało się wyszukać rekordów.")); })
        .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    }, 180);
    return () => { window.clearTimeout(timer); controller.abort(); };
  }, [open, query, field.reference_type, locale]);
  const selected = results.find((item) => item.id === current);
  return (
    <div className="form-field">
      <label htmlFor={`reference-${field.key}`}>{fieldLabel(field, locale)}{field.required ? " *" : ""}</label>
      <div className="flex flex-wrap gap-2">
        <input id={`reference-${field.key}`} autoComplete="off" maxLength={160} value={open ? query : (selected?.label ?? (current ? `#${current}` : ""))} disabled={disabled} required={required && !current} onFocus={() => { setOpen(true); setQuery(""); }} onChange={(event) => { setOpen(true); setQuery(event.target.value); }} aria-expanded={open} aria-controls={`reference-options-${field.key}`} />
        {current && <button type="button" className="button button--small button--secondary" disabled={disabled} onClick={() => { onChange(undefined); setQuery(""); setOpen(true); }} aria-label={locale === "en" ? `Clear ${fieldLabel(field, locale)}` : `Wyczyść ${fieldLabel(field, locale)}`}>×</button>}
      </div>
      {open && !disabled && <div id={`reference-options-${field.key}`} role="listbox" className="mt-1 max-h-48 overflow-auto rounded border border-slate-300 bg-white p-1 shadow-sm">
        {loading && <p className="muted px-2 py-1">{locale === "en" ? "Searching…" : "Wyszukiwanie…"}</p>}
        {error && <p className="notice notice--error" role="alert">{error}</p>}
        {!loading && !error && results.length === 0 && <p className="muted px-2 py-1">{locale === "en" ? "No accessible records." : "Brak dostępnych rekordów."}</p>}
        {results.map((item) => <button key={item.id} type="button" role="option" aria-selected={current === item.id} className="block w-full rounded px-2 py-1 text-left text-sm hover:bg-slate-100" onClick={() => { onChange(item.id); setOpen(false); }}>{item.label}</button>)}
      </div>}
      {open && <button type="button" className="mt-1 text-xs text-slate-600 underline" onClick={() => setOpen(false)}>{locale === "en" ? "Close search" : "Zamknij wyszukiwarkę"}</button>}
      {labelByKey(field, locale) && <small className="muted">{labelByKey(field, locale)}</small>}
    </div>
  );
}

function renderField(field: MetadataField, value: unknown, locale: Locale, disabled: boolean, required: boolean, onChange: (value: unknown) => void) {
  const id = `metadata-${field.key}`;
  const label = fieldLabel(field, locale);
  const description = labelByKey(field, locale);
  const requiredLabel = required ? " *" : "";
  if (field.type === "REFERENCE") return <ReferenceField key={field.key} field={field} value={value} locale={locale} disabled={disabled} required={required} onChange={(next) => onChange(next)} />;
  if (field.type === "BOOLEAN") return <label key={field.key} className="flex items-center gap-2"><input id={id} type="checkbox" checked={Boolean(value)} disabled={disabled} onChange={(event) => onChange(event.target.checked)} /><span>{label}{requiredLabel}</span>{description && <small className="muted">{description}</small>}</label>;
  if (field.type === "CHOICE") return <label key={field.key} htmlFor={id}>{label}{requiredLabel}<select id={id} required={required} value={String(value ?? "")} disabled={disabled} onChange={(event) => onChange(event.target.value || undefined)}><option value="">{locale === "en" ? "Select…" : "Wybierz…"}</option>{field.options?.map((option) => <option key={option.value} value={option.value}>{option.labels[locale] || option.labels.pl || option.value}</option>)}</select>{description && <small className="muted">{description}</small>}</label>;
  if (field.type === "MULTI_CHOICE") return <label key={field.key} htmlFor={id}>{label}{requiredLabel}<select id={id} multiple size={Math.min(5, Math.max(2, field.options?.length ?? 2))} required={required} value={Array.isArray(value) ? value.map(String) : []} disabled={disabled} onChange={(event) => onChange(Array.from(event.currentTarget.selectedOptions, (option) => option.value))}>{field.options?.map((option) => <option key={option.value} value={option.value}>{option.labels[locale] || option.labels.pl || option.value}</option>)}</select>{description && <small className="muted">{description}</small>}</label>;
  if (field.type === "LONG_TEXT") return <label key={field.key} htmlFor={id}>{label}{requiredLabel}<textarea id={id} maxLength={field.max_length} required={required} rows={4} value={String(value ?? "")} disabled={disabled} onChange={(event) => onChange(event.target.value || undefined)} />{description && <small className="muted">{description}</small>}</label>;
  const type = field.type === "INTEGER" || field.type === "DECIMAL" ? "number" : field.type === "DATE" ? "date" : field.type === "DATETIME" ? "datetime-local" : "text";
  const shown = field.type === "DATETIME" && value ? (() => { const date = new Date(String(value)); return Number.isNaN(date.getTime()) ? "" : new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16); })() : String(value ?? "");
  return <label key={field.key} htmlFor={id}>{label}{requiredLabel}<input id={id} type={type} required={required} maxLength={field.max_length} min={field.min === undefined ? undefined : String(field.min)} max={field.max === undefined ? undefined : String(field.max)} step={field.type === "INTEGER" ? 1 : field.type === "DECIMAL" && (field.scale ?? 0) > 0 ? `0.${"0".repeat(Math.max(0, (field.scale ?? 1) - 1))}1` : field.type === "DECIMAL" ? 1 : undefined} value={shown} disabled={disabled} onChange={(event) => {
    const raw = event.target.value;
    if (!raw) { onChange(undefined); return; }
    if (field.type === "DATETIME") { const parsed = new Date(raw); onChange(Number.isNaN(parsed.getTime()) ? raw : parsed.toISOString()); return; }
    onChange(raw);
  }} />{description && <small className="muted">{description}</small>}</label>;
}

export function MetadataFields({ schema, values, baseValues = {}, onChange, locale, disabled = false, filterable = false, filterPrefix = "cf." }: {
  schema: MetadataSchema;
  values: Record<string, unknown>;
  baseValues?: Record<string, unknown>;
  onChange: (values: Record<string, unknown>) => void;
  locale: Locale;
  disabled?: boolean;
  filterable?: boolean;
  filterPrefix?: string;
}) {
  const id = useId();
  const { hasPermission } = useAuth();
  const [activeTab, setActiveTab] = useState("");
  const roleViews = filterable ? schema.list.role_views : schema.form.role_views;
  const matchingViews = Object.entries(roleViews ?? {}).filter(([permission]) => hasPermission(permission));
  const permittedKeys = matchingViews.length ? new Set(matchingViews.flatMap(([, keys]) => keys)) : undefined;
  const fields = schema.fields.filter((field) => field.active && !field.system && (!permittedKeys || permittedKeys.has(field.key)));
  const allValues = { ...baseValues, ...values };
  const visibleFields = fields.filter((field) => evaluateRule(field.visible_when, allValues));
  if (fields.length === 0) return null;
  const change = (key: string, value: unknown) => {
    const next = { ...values };
    if (value === undefined || value === "") delete next[key]; else next[key] = value;
    onChange(next);
  };
  if (filterable) return <fieldset className="form-grid border-t border-slate-200 pt-3"><legend className="font-semibold">{locale === "en" ? "Custom field filters" : "Filtry pól własnych"}</legend>{visibleFields.filter((field) => schema.list.filters.includes(field.key)).map((field) => <div key={field.key} className="form-field">{renderField(field, values[`${filterPrefix}${field.key}`], locale, disabled, false, (value) => change(`${filterPrefix}${field.key}`, value))}</div>)}</fieldset>;
  const groups = schema.form.sections.length > 0
    ? schema.form.sections.map((section) => ({ key: section.key, label: section.labels?.[locale] || section.labels?.pl || section.label, columns: section.columns, fields: section.fields.map((key) => visibleFields.find((field) => field.key === key)).filter((field): field is MetadataField => Boolean(field)) })).filter((section) => section.fields.length > 0)
    : [{ key: "custom", label: locale === "en" ? "Additional information" : "Informacje dodatkowe", columns: 2, fields: visibleFields }];
  const placed = new Set(groups.flatMap((group) => group.fields.map((field) => field.key)));
  const unplaced = visibleFields.filter((field) => !placed.has(field.key));
  if (unplaced.length) groups.push({ key: "additional", label: locale === "en" ? "Additional information" : "Informacje dodatkowe", columns: 2, fields: unplaced });
  const tabbed = groups.filter((group) => schema.form.tabs.includes(group.key));
  const selectedTab = tabbed.some((group) => group.key === activeTab) ? activeTab : (tabbed[0]?.key ?? "");
  const shownGroups = tabbed.length > 1
    ? groups.filter((group) => !schema.form.tabs.includes(group.key) || group.key === selectedTab)
    : groups;
  return <div id={id} className="grid gap-4">
    {tabbed.length > 1 && <div className="tabs" role="tablist" aria-label={locale === "en" ? "Form sections" : "Sekcje formularza"}>{tabbed.map((group) => <button key={group.key} type="button" role="tab" aria-selected={selectedTab === group.key} className={selectedTab === group.key ? "is-active" : ""} onClick={() => setActiveTab(group.key)}>{group.label}</button>)}</div>}
    {shownGroups.map((group) => <fieldset key={group.key} className="form-grid border-t border-slate-200 pt-3"><legend className="font-semibold">{group.label}</legend><div className={`grid grid-cols-1 gap-4 ${group.columns > 1 ? "md:grid-cols-2" : ""}`}>{group.fields.map((field) => { const isRequired = field.required || evaluateRule(field.required_when, allValues); return <div key={field.key} className="form-field">{renderField(field, values[field.key], locale, disabled, isRequired, (value) => change(field.key, value))}</div>; })}</div></fieldset>)}
  </div>;
}

export function requiredWhen(rule: Expression | undefined, values: Record<string, unknown>) { return evaluateRule(rule, values); }
