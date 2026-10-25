import { useAuth } from "../auth/AuthContext";
import { MetadataFields } from "../metadata/MetadataFields";
import { useEffect, useState } from "react";
import { api, ApiError } from "../api/client";
import { useCMDBLocale } from "../cmdb/messages";
import { RemoteCombobox, type Choice } from "../components/RemoteCombobox";
import { UnsavedGuard } from "../incidents/UnsavedGuard";
import { majorMessages } from "../incidents/majorMessages";
import type {
  IncidentProcess,
  IncidentRule,
  IncidentTemplate,
} from "../incidents/majorContracts";
import type { Configuration, MetadataHead } from "../metadata/contracts";

type Draft = Configuration & { incident_process: IncidentProcess };
const blankRule = (): IncidentRule => ({
  key: "",
  name: "",
  active: true,
  group_id: "",
  service_id: "",
  category: "",
  location: "",
  priority: 0,
  contains: "",
  custom_field: "",
});
const blankTemplate = (): IncidentTemplate => ({
  key: "",
  version: "1",
  labels: { pl: "", en: "" },
  retired: false,
  title: "",
  description: "",
  category: "other",
  subcategory: "other",
  impact: 3,
  urgency: 3,
  service_id: "",
  location: "",
});
const choice = (id: string): Choice | null => (id ? { id, label: id } : null);

export function IncidentAdminPage() {
  const locale = useCMDBLocale(),
    t = majorMessages[locale];
  const { hasPermission } = useAuth();
  const canService =
    hasPermission("service_portfolio.read") ||
    hasPermission("service_portfolio.manage");
  const [previewCustom, setPreviewCustom] = useState<Record<string, unknown>>(
    {},
  );
  const [head, setHead] = useState<MetadataHead>(),
    [draft, setDraft] = useState<Draft>(),
    [busy, setBusy] = useState(false),
    [dirty, setDirty] = useState(false),
    [error, setError] = useState<string>(),
    [notice, setNotice] = useState<string>(),
    [latest, setLatest] = useState<MetadataHead>();
  const [preview, setPreview] = useState({
      title: "",
      description: "",
      category: "other",
      impact: 3,
      urgency: 3,
      service_id: "",
      location: "",
    }),
    [match, setMatch] = useState<{
      rule_key: string;
      group_name: string;
      preserves_assignment: boolean;
    }>();
  async function reload(signal?: AbortSignal) {
    setBusy(true);
    setError(undefined);
    try {
      const value = await api<MetadataHead>("/api/v1/metadata/configuration", {
        signal,
      });
      if (signal?.aborted) return;
      setHead(value);
      setDraft(structuredClone(value.draft) as Draft);
      setDirty(false);
      setLatest(undefined);
    } catch (cause) {
      if (!signal?.aborted)
        setError(cause instanceof Error ? cause.message : t.error);
    } finally {
      if (!signal?.aborted) setBusy(false);
    }
  }
  useEffect(() => {
    const controller = new AbortController();
    void reload(controller.signal);
    return () => controller.abort();
  }, []);
  async function captureConflict(cause: unknown) {
    setError(cause instanceof Error ? cause.message : t.error);
    if (cause instanceof ApiError && cause.status === 409) {
      try {
        setLatest(await api<MetadataHead>("/api/v1/metadata/configuration"));
      } catch {
        /* A later refresh can retry the comparison. */
      }
    }
  }
  function rebase() {
    if (!latest || !draft) return;
    const next = structuredClone(latest.draft) as Draft;
    next.incident_process = draft.incident_process;
    next.priority_matrix = draft.priority_matrix;
    for (const [key, value] of Object.entries(draft.dictionaries)) {
      if (key.startsWith("incident.")) next.dictionaries[key] = value;
    }
    setHead(latest);
    setDraft(next);
    setLatest(undefined);
    setDirty(true);
    setError(undefined);
  }
  function dictionaryValue(key: string, index: number, value: string) {
    if (!draft) return;
    const values = draft.dictionaries[key] ?? [];
    const old = values[index]?.value ?? "";
    const dictionaries = {
      ...draft.dictionaries,
      [key]: values.map((v, i) => (i === index ? { ...v, value } : v)),
    };
    if (key === "incident.category" && value) {
      dictionaries[`incident.subcategory.${value}`] = dictionaries[
        `incident.subcategory.${old}`
      ] ?? [{ value: "other", labels: { pl: "Inne", en: "Other" } }];
      if (
        old &&
        !head?.published.dictionaries[key]?.some((o) => o.value === old)
      )
        delete dictionaries[`incident.subcategory.${old}`];
    }
    update({ ...draft, dictionaries });
  }
  function update(next: Draft) {
    setDraft(next);
    setDirty(true);
    setNotice(undefined);
  }
  function process(next: IncidentProcess) {
    if (draft) update({ ...draft, incident_process: next });
  }
  async function save() {
    if (!draft || !head || busy) return;
    setBusy(true);
    setError(undefined);
    try {
      const saved = await api<MetadataHead>(
        "/api/v1/metadata/configuration/draft",
        {
          method: "PUT",
          body: JSON.stringify({ version: head.version, configuration: draft }),
        },
      );
      setHead(saved);
      setDraft(structuredClone(saved.draft) as Draft);
      setDirty(false);
      setNotice(t.saved);
    } catch (cause) {
      await captureConflict(cause);
    } finally {
      setBusy(false);
    }
  }
  async function publish() {
    if (!head || busy || dirty) return;
    setBusy(true);
    setError(undefined);
    try {
      const result = await api<{ head: MetadataHead }>(
        "/api/v1/metadata/configuration/publish",
        { method: "POST", body: JSON.stringify({ version: head.version }) },
      );
      setHead(result.head);
      setDraft(structuredClone(result.head.draft) as Draft);
      setNotice(t.saved);
    } catch (cause) {
      await captureConflict(cause);
    } finally {
      setBusy(false);
    }
  }
  if (!draft || !head)
    return (
      <section className="module08-page">
        <h1>{t.process}</h1>
        {error ? <p role="alert">{error}</p> : <p role="status">{t.loading}</p>}
        <button
          className="button button--secondary"
          disabled={busy}
          onClick={() => void reload()}
        >
          {t.refresh}
        </button>
      </section>
    );
  const p = draft.incident_process;
  const dictionary = (key: string) => draft.dictionaries[key] ?? [];
  const incidentFields = draft.record_types.incident?.fields ?? [];
  const dictionaryKeys = Object.keys(draft.dictionaries).filter((key) =>
    key.startsWith("incident."),
  );
  const changeRule = (index: number, patch: Partial<IncidentRule>) =>
    process({
      ...p,
      assignment_rules: p.assignment_rules.map((rule, i) =>
        i === index ? { ...rule, ...patch } : rule,
      ),
    });
  const changeTemplate = (index: number, patch: Partial<IncidentTemplate>) =>
    process({
      ...p,
      templates: p.templates.map((template, i) =>
        i === index ? { ...template, ...patch } : template,
      ),
    });
  const moveRule = (index: number, delta: number) => {
    const rules = [...p.assignment_rules];
    const target = index + delta;
    if (target < 0 || target >= rules.length) return;
    const a = rules[index],
      b = rules[target];
    if (!a || !b) return;
    rules[index] = b;
    rules[target] = a;
    process({ ...p, assignment_rules: rules });
  };
  return (
    <section className="module08-page">
      <UnsavedGuard dirty={dirty} />
      <header className="section-header">
        <h1>{t.process}</h1>
        <span>
          {t.version}: {head.published_version} · {t.draft}:{" "}
          {head.draft_version || "—"}
        </span>
      </header>
      {error && (
        <p role="alert" className="notice notice--error">
          {error}
        </p>
      )}
      {latest && (
        <section className="notice">
          <p>{t.conflict}</p>
          <details>
            <summary>{t.currentConfiguration}</summary>
            <pre>
              {JSON.stringify(
                {
                  incident_process: latest.draft.incident_process,
                  priority_matrix: latest.draft.priority_matrix,
                  dictionaries: Object.fromEntries(
                    Object.entries(latest.draft.dictionaries).filter(([key]) =>
                      key.startsWith("incident."),
                    ),
                  ),
                },
                null,
                2,
              )}
            </pre>
          </details>
          <button
            className="button button--secondary"
            disabled={busy}
            onClick={rebase}
          >
            {t.rebaseDraft}
          </button>
        </section>
      )}
      {notice && (
        <p role="status" className="notice">
          {notice}
        </p>
      )}
      <div className="action-row">
        <button
          className="button button--primary"
          disabled={busy || !dirty}
          onClick={() => void save()}
        >
          {t.saveDraft}
        </button>
        <button
          className="button button--secondary"
          disabled={
            busy ||
            dirty ||
            !head.draft_version ||
            head.draft_version === head.published_version
          }
          onClick={() => void publish()}
        >
          {t.publishConfig}
        </button>
      </div>
      <fieldset className="form-fields" disabled={busy}>
        <h2>{t.rules}</h2>
        {!canService && <p>{t.servicePermission}</p>}
        <RemoteCombobox
          kind="groups"
          label={t.defaultQueue}
          endpoint="/api/v1/incidents/lookups/groups"
          value={choice(p.default_group_id)}
          onChange={(v) => process({ ...p, default_group_id: v?.id ?? "" })}
        />
        {p.assignment_rules.map((rule, index) => (
          <fieldset key={index} className="module08-rule">
            <legend>
              {index + 1}. {rule.name || t.ruleName}
            </legend>
            <div className="two-column">
              <label>
                {t.key}
                <input
                  required
                  pattern="[a-z][a-z0-9_]*"
                  maxLength={48}
                  value={rule.key}
                  onChange={(e) => changeRule(index, { key: e.target.value })}
                />
              </label>
              <label>
                {t.ruleName}
                <input
                  value={rule.name}
                  maxLength={160}
                  onChange={(e) => changeRule(index, { name: e.target.value })}
                />
              </label>
            </div>
            <label>
              <input
                type="checkbox"
                checked={rule.active}
                onChange={(e) =>
                  changeRule(index, { active: e.target.checked })
                }
              />
              {t.active}
            </label>
            <RemoteCombobox
              kind="groups"
              label={t.defaultQueue}
              endpoint="/api/v1/incidents/lookups/groups"
              value={choice(rule.group_id)}
              onChange={(v) => changeRule(index, { group_id: v?.id ?? "" })}
            />
            {canService && (
              <RemoteCombobox
                kind="users"
                collection="items"
                cursor="before_id"
                label={t.service}
                endpoint="/api/v1/service-portfolio/items?lifecycle=ACTIVE"
                value={choice(rule.service_id)}
                onChange={(v) => changeRule(index, { service_id: v?.id ?? "" })}
              />
            )}
            <div className="two-column">
              <label>
                {t.category}
                <select
                  value={rule.category}
                  onChange={(e) =>
                    changeRule(index, { category: e.target.value })
                  }
                >
                  <option value="">—</option>
                  {dictionary("incident.category")
                    .filter((o) => !o.retired)
                    .map((o) => (
                      <option key={o.value} value={o.value}>
                        {o.labels[locale]}
                      </option>
                    ))}
                </select>
              </label>
              <label>
                {t.location}
                <input
                  maxLength={160}
                  value={rule.location}
                  onChange={(e) =>
                    changeRule(index, { location: e.target.value })
                  }
                />
              </label>
              <label>
                {t.priority}
                <input
                  type="number"
                  min={0}
                  max={5}
                  value={rule.priority}
                  onChange={(e) =>
                    changeRule(index, { priority: Number(e.target.value) })
                  }
                />
              </label>
              <label>
                {t.contains}
                <input
                  maxLength={200}
                  value={rule.contains}
                  onChange={(e) =>
                    changeRule(index, { contains: e.target.value })
                  }
                />
              </label>
              <label>
                {t.customField}
                <select
                  value={rule.custom_field}
                  onChange={(e) =>
                    changeRule(index, {
                      custom_field: e.target.value,
                      custom_value: e.target.value ? "" : undefined,
                    })
                  }
                >
                  <option value="">—</option>
                  {incidentFields
                    .filter((f) => f.active)
                    .map((f) => (
                      <option key={f.key} value={f.key}>
                        {f.labels[locale]}
                      </option>
                    ))}
                </select>
              </label>
              {rule.custom_field && (
                <MetadataFields
                  locale={locale}
                  schema={{
                    type_key: "incident",
                    version: head.published_version,
                    fields: incidentFields
                      .filter((f) => f.key === rule.custom_field)
                      .map((f) => ({
                        ...f,
                        required: false,
                        visible_when: undefined,
                        required_when: undefined,
                      })),
                    form: { sections: [], tabs: [] },
                    list: {
                      columns: [],
                      filters: [],
                      search_fields: [],
                      default_sort: "",
                    },
                  }}
                  values={{ [rule.custom_field]: rule.custom_value }}
                  onChange={(values) =>
                    changeRule(index, {
                      custom_value: values[rule.custom_field],
                    })
                  }
                />
              )}
            </div>
            <div className="action-row">
              <button
                className="button button--secondary"
                type="button"
                disabled={index === 0}
                onClick={() => moveRule(index, -1)}
              >
                {t.up}
              </button>
              <button
                className="button button--secondary"
                type="button"
                disabled={index === p.assignment_rules.length - 1}
                onClick={() => moveRule(index, 1)}
              >
                {t.down}
              </button>
              <button
                className="button button--secondary"
                type="button"
                onClick={() =>
                  process({
                    ...p,
                    assignment_rules: p.assignment_rules.filter(
                      (_, i) => i !== index,
                    ),
                  })
                }
              >
                {t.remove}
              </button>
            </div>
          </fieldset>
        ))}
        <button
          className="button button--secondary"
          type="button"
          disabled={p.assignment_rules.length >= 100}
          onClick={() =>
            process({
              ...p,
              assignment_rules: [...p.assignment_rules, blankRule()],
            })
          }
        >
          {t.add}
        </button>
        <h2>{t.autoClose}</h2>
        <input
          aria-label={t.autoClose}
          type="number"
          min={0}
          max={8760}
          value={p.auto_close_hours}
          onChange={(e) =>
            process({ ...p, auto_close_hours: Number(e.target.value) })
          }
        />
        <label>
          {t.reopen}
          <input
            type="number"
            min={0}
            max={8760}
            value={p.reopen_hours}
            onChange={(e) =>
              process({ ...p, reopen_hours: Number(e.target.value) })
            }
          />
        </label>
        <label>
          <input
            type="checkbox"
            checked={p.requester_can_reopen}
            onChange={(e) =>
              process({ ...p, requester_can_reopen: e.target.checked })
            }
          />
          {t.requesterReopen}
        </label>
        <h3>{t.requiredClose}</h3>
        {[
          "title",
          "description",
          "service_id",
          "location",
          "resolution_code",
          "resolution_description",
          ...incidentFields
            .filter((f) => f.active)
            .map((f) => `custom.${f.key}`),
        ].map((key) => (
          <label key={key}>
            <input
              type="checkbox"
              checked={p.close_required_fields.includes(key)}
              onChange={(e) =>
                process({
                  ...p,
                  close_required_fields: e.target.checked
                    ? [...p.close_required_fields, key]
                    : p.close_required_fields.filter((x) => x !== key),
                })
              }
            />
            {(
              {
                title: t.titleField,
                description: t.description,
                service_id: t.service,
                location: t.location,
                resolution_code: t.resolutionCode,
                resolution_description: t.resolutionDescription,
              } as Record<string, string>
            )[key] ??
              incidentFields.find((f) => `custom.${f.key}` === key)?.labels[
                locale
              ] ??
              key}
          </label>
        ))}
        <h3>{t.transitions}</h3>
        {Object.entries({
          NEW: ["IN_PROGRESS", "ON_HOLD", "CANCELLED"],
          IN_PROGRESS: ["ON_HOLD", "RESOLVED", "CANCELLED"],
          ON_HOLD: ["IN_PROGRESS", "RESOLVED", "CANCELLED"],
          RESOLVED: ["CLOSED", "IN_PROGRESS"],
          CLOSED: ["IN_PROGRESS"],
        }).map(([from, targets]) => (
          <fieldset key={from}>
            <legend>{from}</legend>
            {targets.map((to) => (
              <label key={to}>
                <input
                  type="checkbox"
                  checked={p.transitions[from]?.includes(to) ?? false}
                  onChange={(e) =>
                    process({
                      ...p,
                      transitions: {
                        ...p.transitions,
                        [from]: e.target.checked
                          ? [...(p.transitions[from] ?? []), to]
                          : (p.transitions[from] ?? []).filter((v) => v !== to),
                      },
                    })
                  }
                />
                {to}
              </label>
            ))}
          </fieldset>
        ))}
        <h2>{t.template}</h2>
        {p.templates.map((template, index) => (
          <fieldset key={index} className="module08-rule">
            <legend>{template.labels[locale] || t.template}</legend>
            <div className="two-column">
              <label>
                {t.key}
                <input
                  maxLength={48}
                  pattern="[a-z][a-z0-9_]*"
                  disabled={head.published.incident_process?.templates.some(
                    (v) => v.key === template.key,
                  )}
                  value={template.key}
                  onChange={(e) =>
                    changeTemplate(index, { key: e.target.value })
                  }
                />
              </label>
              <label>
                {t.version}
                <input
                  inputMode="numeric"
                  pattern="[1-9][0-9]*"
                  value={template.version}
                  onChange={(e) =>
                    changeTemplate(index, { version: e.target.value })
                  }
                />
              </label>
              {(["pl", "en"] as const).map((lang) => (
                <label key={lang}>
                  {lang === "pl" ? t.plLabel : t.enLabel}
                  <input
                    maxLength={160}
                    value={template.labels[lang]}
                    onChange={(e) =>
                      changeTemplate(index, {
                        labels: { ...template.labels, [lang]: e.target.value },
                      })
                    }
                  />
                </label>
              ))}
            </div>
            <label>
              <input
                type="checkbox"
                checked={template.retired}
                onChange={(e) =>
                  changeTemplate(index, { retired: e.target.checked })
                }
              />
              {t.retired}
            </label>
            <label>
              {t.titleField}
              <input
                maxLength={200}
                value={template.title}
                onChange={(e) =>
                  changeTemplate(index, { title: e.target.value })
                }
              />
            </label>
            <label>
              {t.description}
              <textarea
                maxLength={20000}
                value={template.description}
                onChange={(e) =>
                  changeTemplate(index, { description: e.target.value })
                }
              />
            </label>
            <div className="two-column">
              <label>
                {t.category}
                <select
                  value={template.category}
                  onChange={(e) =>
                    changeTemplate(index, {
                      category: e.target.value,
                      subcategory:
                        draft.dictionaries[
                          `incident.subcategory.${e.target.value}`
                        ]?.find((o) => !o.retired)?.value ?? "",
                    })
                  }
                >
                  {dictionary("incident.category").map((o) => (
                    <option key={o.value} value={o.value}>
                      {o.labels[locale]}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                {t.subcategory}
                <select
                  value={template.subcategory}
                  onChange={(e) =>
                    changeTemplate(index, { subcategory: e.target.value })
                  }
                >
                  {(
                    draft.dictionaries[
                      `incident.subcategory.${template.category}`
                    ] ?? []
                  ).map((o) => (
                    <option key={o.value} value={o.value}>
                      {o.labels[locale]}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                {t.impactLevel}
                <input
                  type="number"
                  min={1}
                  max={3}
                  value={template.impact}
                  onChange={(e) =>
                    changeTemplate(index, { impact: Number(e.target.value) })
                  }
                />
              </label>
              <label>
                {t.urgency}
                <input
                  type="number"
                  min={1}
                  max={3}
                  value={template.urgency}
                  onChange={(e) =>
                    changeTemplate(index, { urgency: Number(e.target.value) })
                  }
                />
              </label>
            </div>
            {canService && (
              <RemoteCombobox
                kind="users"
                collection="items"
                cursor="before_id"
                label={t.service}
                endpoint="/api/v1/service-portfolio/items?lifecycle=ACTIVE"
                value={choice(template.service_id)}
                onChange={(v) =>
                  changeTemplate(index, { service_id: v?.id ?? "" })
                }
              />
            )}
            <MetadataFields
              locale={locale}
              schema={{
                type_key: "incident",
                version: head.published_version,
                fields: incidentFields.map((f) => ({
                  ...f,
                  required: false,
                  required_when: undefined,
                })),
                form: draft.record_types.incident!.form,
                list: draft.record_types.incident!.list,
              }}
              values={template.custom_fields ?? {}}
              baseValues={template}
              onChange={(values) =>
                changeTemplate(index, { custom_fields: values })
              }
            />
            <label>
              {t.location}
              <input
                maxLength={160}
                value={template.location}
                onChange={(e) =>
                  changeTemplate(index, { location: e.target.value })
                }
              />
            </label>
          </fieldset>
        ))}
        <button
          className="button button--secondary"
          type="button"
          disabled={p.templates.length >= 100}
          onClick={() =>
            process({ ...p, templates: [...p.templates, blankTemplate()] })
          }
        >
          {t.add}
        </button>
        <h2>{t.dictionaries}</h2>
        {dictionaryKeys.map((key) => (
          <details key={key}>
            <summary>{key}</summary>
            {dictionary(key).map((option, index) => (
              <div className="module08-dictionary" key={index}>
                <label>
                  {t.key}
                  <input
                    maxLength={40}
                    disabled={head.published.dictionaries[key]?.some(
                      (v) => v.value === option.value,
                    )}
                    value={option.value}
                    onChange={(e) =>
                      dictionaryValue(key, index, e.target.value)
                    }
                  />
                </label>
                {(["pl", "en"] as const).map((lang) => (
                  <label key={lang}>
                    {lang === "pl" ? t.plLabel : t.enLabel}
                    <input
                      maxLength={160}
                      value={option.labels[lang]}
                      onChange={(e) =>
                        update({
                          ...draft,
                          dictionaries: {
                            ...draft.dictionaries,
                            [key]: dictionary(key).map((v, i) =>
                              i === index
                                ? {
                                    ...v,
                                    labels: {
                                      ...v.labels,
                                      [lang]: e.target.value,
                                    },
                                  }
                                : v,
                            ),
                          },
                        })
                      }
                    />
                  </label>
                ))}
                {key !== "incident.state" && (
                  <label>
                    <input
                      type="checkbox"
                      checked={Boolean(option.retired)}
                      onChange={(e) =>
                        update({
                          ...draft,
                          dictionaries: {
                            ...draft.dictionaries,
                            [key]: dictionary(key).map((v, i) =>
                              i === index
                                ? { ...v, retired: e.target.checked }
                                : v,
                            ),
                          },
                        })
                      }
                    />
                    {t.retired}
                  </label>
                )}
              </div>
            ))}
            {key !== "incident.state" && (
              <button
                className="button button--secondary"
                type="button"
                onClick={() => {
                  const next = {
                    ...draft.dictionaries,
                    [key]: [
                      ...dictionary(key),
                      { value: "", labels: { pl: "", en: "" } },
                    ],
                  };
                  update({ ...draft, dictionaries: next });
                }}
              >
                {t.add}
              </button>
            )}
          </details>
        ))}
        <h2>{t.matrix}</h2>
        <div className="table-scroll">
          <table>
            <tbody>
              {draft.priority_matrix.map((row, ri) => (
                <tr key={ri}>
                  {row.map((value, ci) => (
                    <td key={ci}>
                      <input
                        aria-label={`${t.matrix} ${ri + 1}, ${ci + 1}`}
                        type="number"
                        min={1}
                        max={5}
                        value={value}
                        onChange={(e) =>
                          update({
                            ...draft,
                            priority_matrix: draft.priority_matrix.map(
                              (r, i) =>
                                i === ri
                                  ? r.map((v, j) =>
                                      j === ci ? Number(e.target.value) : v,
                                    )
                                  : r,
                            ),
                          })
                        }
                      />
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </fieldset>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          setBusy(true);
          setError(undefined);
          void api<{
            rule_key: string;
            group_name: string;
            preserves_assignment: boolean;
          }>("/api/v1/incidents/assignment-preview", {
            method: "POST",
            body: JSON.stringify({
              ...preview,
              custom_fields: previewCustom,
              service_id: preview.service_id || null,
            }),
          })
            .then(setMatch)
            .catch((cause) =>
              setError(cause instanceof Error ? cause.message : t.error),
            )
            .finally(() => setBusy(false));
        }}
      >
        <h2>{t.preview}</h2>
        <p>{t.previewPublished}</p>
        <fieldset className="form-fields" disabled={busy}>
          <label>
            {t.titleField}
            <input
              value={preview.title}
              onChange={(e) =>
                setPreview({ ...preview, title: e.target.value })
              }
            />
          </label>
          <label>
            {t.category}
            <select
              value={preview.category}
              onChange={(e) =>
                setPreview({ ...preview, category: e.target.value })
              }
            >
              {dictionary("incident.category").map((o) => (
                <option key={o.value} value={o.value}>
                  {o.labels[locale]}
                </option>
              ))}
            </select>
          </label>
          <label>
            {t.description}
            <textarea
              value={preview.description}
              onChange={(e) =>
                setPreview({ ...preview, description: e.target.value })
              }
            />
          </label>
          {canService && (
            <RemoteCombobox
              kind="users"
              label={t.service}
              collection="items"
              cursor="before_id"
              endpoint="/api/v1/service-portfolio/items?lifecycle=ACTIVE"
              value={choice(preview.service_id)}
              onChange={(v) =>
                setPreview({ ...preview, service_id: v?.id ?? "" })
              }
            />
          )}
          <label>
            {t.location}
            <input
              value={preview.location}
              onChange={(e) =>
                setPreview({ ...preview, location: e.target.value })
              }
            />
          </label>
          <label>
            {t.impactLevel}
            <input
              type="number"
              min={1}
              max={3}
              value={preview.impact}
              onChange={(e) =>
                setPreview({ ...preview, impact: Number(e.target.value) })
              }
            />
          </label>
          <label>
            {t.urgency}
            <input
              type="number"
              min={1}
              max={3}
              value={preview.urgency}
              onChange={(e) =>
                setPreview({ ...preview, urgency: Number(e.target.value) })
              }
            />
          </label>
          <MetadataFields
            locale={locale}
            schema={{
              type_key: "incident",
              version: head.published_version,
              fields: head.published.record_types.incident!.fields,
              form: head.published.record_types.incident!.form,
              list: head.published.record_types.incident!.list,
            }}
            values={previewCustom}
            baseValues={preview}
            onChange={setPreviewCustom}
          />
          <button className="button button--secondary">{t.preview}</button>
        </fieldset>
        {match && (
          <p role="status">
            {match.group_name || t.noMatch} · {match.rule_key}
            {match.preserves_assignment && ` · ${t.preserved}`}
          </p>
        )}
      </form>
    </section>
  );
}
