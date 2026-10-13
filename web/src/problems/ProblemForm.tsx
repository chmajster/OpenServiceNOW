import { useState } from "react";
import { InventoryChoice } from "../cmdb/components";
import { labels, type ProblemFields } from "./contracts";
export function ProblemForm({
  draft,
  change,
  disabled,
  canAssign = true,
  canKnownError = true,
  assignedName = "",
  groupName = "",
}: {
  draft: ProblemFields;
  change: (v: ProblemFields) => void;
  disabled: boolean;
  canAssign?: boolean;
  canKnownError?: boolean;
  assignedName?: string;
  groupName?: string;
}) {
  const [names, setNames] = useState<Record<string, string>>({
    ["u:" + draft.assigned_to]: assignedName,
    ["g:" + draft.assignment_group_id]: groupName,
  });
  const set = (key: keyof ProblemFields, value: unknown) =>
    change({ ...draft, [key]: value });
  return (
    <fieldset disabled={disabled} className="form-fields">
      <div className="form-grid">
        <label>
          Opis skrócony
          <input
            required
            maxLength={160}
            value={draft.title}
            onChange={(e) => set("title", e.target.value)}
          />
        </label>
        <label>
          Wpływ
          <select
            aria-label="Wpływ problemu"
            value={draft.impact}
            onChange={(e) => set("impact", e.target.value)}
          >
            {["HIGH", "MEDIUM", "LOW"].map((v) => (
              <option key={v} value={v}>
                {labels[v]}
              </option>
            ))}
          </select>
        </label>
        <InventoryChoice
          label="Grupa odpowiedzialna"
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
          disabled={disabled || !canAssign}
          endpoint="/api/v1/problems/lookups/groups"
          onChange={(v) => {
            if (v) setNames((n) => ({ ...n, ["g:" + v.id]: v.label }));
            set("assignment_group_id", v?.id ?? null);
          }}
        />
        <InventoryChoice
          label="Przypisany analityk"
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
          disabled={disabled || !canAssign}
          endpoint="/api/v1/problems/lookups/users"
          onChange={(v) => {
            if (v) setNames((n) => ({ ...n, ["u:" + v.id]: v.label }));
            set("assigned_to", v?.id ?? null);
          }}
        />
      </div>
      <label>
        Opis problemu
        <textarea
          required
          maxLength={20000}
          rows={3}
          value={draft.description}
          onChange={(e) => set("description", e.target.value)}
        />
      </label>
      <div className="form-grid">
        {(
          [
            ["symptoms", "Objawy"],
            ["analysis", "Wyniki analizy"],
            ["root_cause", "Przyczyna źródłowa"],
            ["workaround", "Obejście"],
            ["solution_plan", "Plan trwałego rozwiązania"],
            ["verification", "Wynik weryfikacji"],
          ] as const
        ).map(([key, label]) => (
          <label key={key}>
            {label}
            <textarea
              maxLength={20000}
              rows={3}
              value={draft[key]}
              onChange={(e) => set(key, e.target.value)}
            />
          </label>
        ))}
      </div>
      <label className="checkbox-label">
        <input
          type="checkbox"
          checked={draft.known_error}
          disabled={disabled || !canKnownError}
          onChange={(e) => set("known_error", e.target.checked)}
        />
        Known Error — udokumentowana przyczyna i obejście
      </label>
    </fieldset>
  );
}
