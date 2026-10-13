import { CISet } from "./CISet";
import { useIncidentQuery } from "../incidents/hooks";
import { useState } from "react";
import { api } from "../api/client";
import { InventoryChoice } from "../cmdb/components";
import type { Config } from "../workflow/contracts";
import { DateTimeField } from "./DateTimeField";
import {
  labels,
  type ChangeFields,
  type TemplateConfig,
  type TemplateSummary,
} from "./contracts";
import { useRemotePage } from "../components/useRemotePage";
import { LoadState } from "../requests/LoadState";

export function ChangeForm({
  draft,
  change,
  disabled,
  canAssign = true,
  assignedName = "",
  groupName = "",
  existing = false,
  plansLocked = false,
  templateMode = false,
}: {
  draft: ChangeFields;
  change: (v: ChangeFields) => void;
  disabled: boolean;
  canAssign?: boolean;
  assignedName?: string;
  groupName?: string;
  existing?: boolean;
  plansLocked?: boolean;
  templateMode?: boolean;
}) {
  const [names, setNames] = useState<Record<string, string>>({
    ["u:" + draft.assigned_to]: assignedName,
    ["g:" + draft.assignment_group_id]: groupName,
  });
  const [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  const set = (k: keyof ChangeFields, v: unknown) =>
    change({ ...draft, [k]: v });
  const templates = useRemotePage<TemplateSummary>(
    "/api/v1/changes/templates?limit=25",
    "items",
    "before_id",
    draft.change_type === "STANDARD" && !templateMode,
  );
  const selectedTemplate = useIncidentQuery<TemplateConfig>(
    !templateMode && draft.template_id && draft.template_version
      ? `/api/v1/changes/templates/${draft.template_id}/versions/${draft.template_version}`
      : null,
  );
  const locked = disabled || busy;
  return (
    <fieldset disabled={locked} className="form-fields">
      <div className="form-grid">
        <label>
          Opis skrócony
          <input
            disabled={plansLocked}
            required
            maxLength={160}
            value={draft.title}
            onChange={(e) => set("title", e.target.value)}
          />
        </label>
        <label>
          Typ zmiany
          <select
            disabled={existing || templateMode}
            value={draft.change_type}
            onChange={(e) =>
              change({
                ...draft,
                change_type: e.target.value,
                workflow_id: null,
                workflow_version: null,
                template_id: null,
                template_version: null,
              })
            }
          >
            {["NORMAL", "STANDARD", "EMERGENCY"].map((v) => (
              <option key={v} value={v}>
                {labels[v]}
              </option>
            ))}
          </select>
        </label>
        {["impact", "risk"].map((k) => (
          <label key={k}>
            {k === "impact" ? "Wpływ" : "Ryzyko"}
            <select
              disabled={plansLocked}
              value={draft[k as "risk"]}
              onChange={(e) => set(k as "risk", e.target.value)}
            >
              {["HIGH", "MEDIUM", "LOW"].map((v) => (
                <option key={v} value={v}>
                  {labels[v]}
                </option>
              ))}
            </select>
          </label>
        ))}
        <InventoryChoice
          label="Grupa odpowiedzialna"
          endpoint="/api/v1/changes/lookups/groups"
          disabled={locked || !canAssign || plansLocked}
          value={
            draft.assignment_group_id
              ? {
                  id: draft.assignment_group_id,
                  label:
                    names["g:" + draft.assignment_group_id] ||
                    groupName ||
                    `Grupa #${draft.assignment_group_id}`,
                }
              : null
          }
          onChange={(v) => {
            if (v) setNames((n) => ({ ...n, ["g:" + v.id]: v.label }));
            set("assignment_group_id", v?.id ?? null);
          }}
        />
        <InventoryChoice
          label="Przypisany wykonawca"
          endpoint="/api/v1/changes/lookups/users"
          disabled={locked || !canAssign || plansLocked}
          value={
            draft.assigned_to
              ? {
                  id: draft.assigned_to,
                  label:
                    names["u:" + draft.assigned_to] ||
                    assignedName ||
                    `Użytkownik #${draft.assigned_to}`,
                }
              : null
          }
          onChange={(v) => {
            if (v) setNames((n) => ({ ...n, ["u:" + v.id]: v.label }));
            set("assigned_to", v?.id ?? null);
          }}
        />
      </div>
      <label>
        Opis zmiany
        <textarea
          required
          maxLength={20000}
          rows={3}
          disabled={plansLocked}
          value={draft.description}
          onChange={(e) => set("description", e.target.value)}
        />
      </label>
      {draft.change_type === "STANDARD" && !templateMode && (
        <section className="panel">
          <h3>Zatwierdzony szablon Standard</h3>
          <LoadState
            loading={templates.loading}
            error={templates.error}
            retry={templates.retry}
          />
          <label>
            Wersja szablonu
            <select
              required
              disabled={plansLocked}
              value={
                draft.template_id && draft.template_version
                  ? `${draft.template_id}:${draft.template_version}`
                  : ""
              }
              onChange={async (e) => {
                const [id, version] = e.target.value.split(":");
                if (!id || !version) return;
                setBusy(true);
                setError("");
                try {
                  const c = await api<TemplateConfig>(
                    `/api/v1/changes/templates/${id}/versions/${version}`,
                  );
                  change({
                    ...c.fields,
                    title: draft.title || c.fields.title,
                    description: draft.description || c.fields.description,
                    planned_start: draft.planned_start,
                    planned_end: draft.planned_end,
                    template_id: id,
                    template_version: version,
                  });
                } catch (err) {
                  setError(
                    err instanceof Error
                      ? err.message
                      : "Nie pobrano szablonu.",
                  );
                } finally {
                  setBusy(false);
                }
              }}
            >
              <option value="">Wybierz zatwierdzoną wersję</option>
              {draft.template_id &&
                !templates.items.some(
                  (t) =>
                    t.id === draft.template_id &&
                    t.published_version === draft.template_version,
                ) && (
                  <option
                    value={`${draft.template_id}:${draft.template_version}`}
                  >
                    Szablon #{draft.template_id} · wersja{" "}
                    {draft.template_version}
                  </option>
                )}
              {templates.items
                .filter((t) => t.published_version && !t.withdrawn)
                .map((t) => (
                  <option key={t.id} value={`${t.id}:${t.published_version}`}>
                    {t.name} · wersja {t.published_version}
                  </option>
                ))}
            </select>
          </label>
          {templates.next && (
            <button type="button" onClick={() => void templates.more()}>
              Więcej szablonów
            </button>
          )}
          {draft.template_id && (
            <>
              <LoadState
                loading={selectedTemplate.loading}
                error={selectedTemplate.error}
                retry={selectedTemplate.reload}
              />
              {selectedTemplate.data && (
                <>
                  <p>
                    Maksymalne ryzyko: {labels[selectedTemplate.data.max_risk]}{" "}
                    · Maksymalny czas:{" "}
                    {selectedTemplate.data.max_duration_seconds} s
                  </p>
                  <CISet ids={selectedTemplate.data.ci_ids} />
                </>
              )}
            </>
          )}
          <p>
            Powiąż dokładny zestaw CI szablonu. Każda zmiana Standard nadal
            wymaga decyzji w przypiętym procesie.
          </p>
        </section>
      )}
      <InventoryChoice
        label="Opublikowany proces akceptacji"
        endpoint={`/api/v1/changes/approval-processes?change_type=${draft.change_type}`}
        disabled={locked || plansLocked}
        value={
          draft.workflow_id
            ? {
                id: draft.workflow_id,
                label:
                  names["w:" + draft.workflow_id] ||
                  `Proces #${draft.workflow_id} · wersja ${draft.workflow_version}`,
              }
            : null
        }
        onChange={async (v) => {
          if (!v) {
            change({ ...draft, workflow_id: null, workflow_version: null });
            return;
          }
          setBusy(true);
          setError("");
          try {
            const selected = await api<{ config: Config; version: string }>(
              `/api/v1/changes/approval-processes/${v.id}`,
            );
            setNames((n) => ({ ...n, ["w:" + v.id]: v.label }));
            change({
              ...draft,
              workflow_id: v.id,
              workflow_version: selected.version,
              assignment_group_id: selected.config.group_id,
            });
          } catch (err) {
            setError(
              err instanceof Error ? err.message : "Nie pobrano procesu.",
            );
          } finally {
            setBusy(false);
          }
        }}
      />
      {error && <p role="alert">{error}</p>}
      <div className="form-grid">
        {(
          [
            ["justification", "Uzasadnienie zmiany"],
            ["scope", "Zakres zmiany"],
            ["implementation_plan", "Plan wdrożenia"],
            ["test_plan", "Plan testów"],
            ["rollback_plan", "Plan wycofania"],
          ] as const
        ).map(([key, label]) => (
          <label key={key}>
            {label}
            <textarea
              maxLength={20000}
              rows={3}
              disabled={plansLocked}
              value={draft[key]}
              onChange={(e) => set(key, e.target.value)}
            />
          </label>
        ))}
        {draft.change_type === "EMERGENCY" && (
          <label>
            Uzasadnienie pilności
            <textarea
              maxLength={20000}
              rows={3}
              disabled={plansLocked}
              value={draft.emergency_reason}
              onChange={(e) => set("emergency_reason", e.target.value)}
            />
          </label>
        )}
      </div>
      {!templateMode && (
        <fieldset disabled={plansLocked} className="form-grid">
          <label>
            Strefa czasowa IANA
            <input
              required
              maxLength={64}
              value={draft.time_zone}
              onChange={(e) => set("time_zone", e.target.value)}
              onBlur={() => {
                try {
                  new Intl.DateTimeFormat("pl-PL", {
                    timeZone: draft.time_zone,
                  }).format();
                } catch {
                  setError("Podaj poprawną strefę IANA, np. Europe/Warsaw.");
                }
              }}
            />
          </label>
          <DateTimeField
            label="Planowany początek"
            value={draft.planned_start}
            zone={draft.time_zone}
            onChange={(v) => set("planned_start", v)}
          />
          <DateTimeField
            label="Planowany koniec"
            value={draft.planned_end}
            zone={draft.time_zone}
            onChange={(v) => set("planned_end", v)}
          />
        </fieldset>
      )}
      {!templateMode && (
        <div className="form-grid">
          <label>
            Wynik przeglądu
            <select
              value={draft.review_outcome}
              onChange={(e) => set("review_outcome", e.target.value)}
            >
              <option value="">Nie zapisano</option>
              {["SUCCESS", "PARTIAL", "FAILED"].map((v) => (
                <option key={v} value={v}>
                  {labels[v]}
                </option>
              ))}
            </select>
          </label>
          <label>
            Opis przeglądu
            <textarea
              maxLength={20000}
              rows={3}
              value={draft.review_result}
              onChange={(e) => set("review_result", e.target.value)}
            />
          </label>
        </div>
      )}
    </fieldset>
  );
}
