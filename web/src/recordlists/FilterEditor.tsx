import { useState } from "react";
import { RemoteCombobox } from "../components/RemoteCombobox";
import { type FilterField, type FilterNode } from "./contracts";
const operators: Record<string, string> = {
  eq: "jest równe",
  ne: "nie jest równe",
  contains: "zawiera",
  starts_with: "zaczyna się od",
  empty: "jest puste",
  not_empty: "nie jest puste",
  gt: "większe niż",
  gte: "co najmniej",
  lt: "mniejsze niż",
  lte: "co najwyżej",
};
const condition = (fields: FilterField[]): FilterNode => ({
  field: fields[0]!.key,
  operator: "eq",
  value: "",
});
export function FilterEditor({
  fields,
  initial,
  apply,
}: {
  fields: FilterField[];
  initial?: FilterNode;
  apply: (n?: FilterNode) => void;
}) {
  const [draft, setDraft] = useState<FilterNode>(
    () => initial ?? { logic: "AND", children: [condition(fields)] },
  );
  const [error, setError] = useState("");
  function submit() {
    let count = 0;
    const validate = (n: FilterNode, depth = 0): boolean => {
      count++;
      if (count > 40 || depth > 5) return false;
      if (n.logic)
        return (
          !!n.children?.length &&
          n.children.length <= 12 &&
          n.children.every((c) => validate(c, depth + 1))
        );
      return (
        !!n.field &&
        !!n.operator &&
        (["empty", "not_empty"].includes(n.operator) || !!n.value)
      );
    };
    if (!validate(draft)) {
      setError(
        "Uzupełnij warunki. Maksymalnie 40 warunków, 12 elementów grupy i 5 poziomów.",
      );
      return;
    }
    setError("");
    apply(draft);
  }
  return (
    <details className="filter-editor">
      <summary>Edytor filtrów AND / OR</summary>
      <p className="muted">
        Warunki obejmują cały dostępny zbiór. Grupa OR oznacza dowolny pasujący
        warunek.
      </p>
      <NodeEditor fields={fields} node={draft} update={setDraft} depth={0} />
      {error && <p role="alert">{error}</p>}
      <div className="action-row">
        <button className="button" onClick={submit}>
          Zastosuj warunek
        </button>
        <button
          className="button button--secondary"
          onClick={() => {
            setDraft({ logic: "AND", children: [condition(fields)] });
            apply(undefined);
          }}
        >
          Usuń warunek
        </button>
      </div>
    </details>
  );
}
function NodeEditor({
  fields,
  node,
  update,
  depth,
}: {
  fields: FilterField[];
  node: FilterNode;
  update: (n: FilterNode) => void;
  depth: number;
}) {
  if (node.logic)
    return (
      <fieldset className="filter-group">
        <legend>Grupa warunków</legend>
        <label>
          Łączenie warunków
          <select
            value={node.logic}
            onChange={(e) =>
              update({ ...node, logic: e.target.value as "AND" | "OR" })
            }
          >
            <option value="AND">AND — wszystkie</option>
            <option value="OR">OR — dowolny</option>
          </select>
        </label>
        {node.children?.map((child, i) => (
          <div className="filter-child" key={i}>
            <NodeEditor
              fields={fields}
              node={child}
              depth={depth + 1}
              update={(n) =>
                update({
                  ...node,
                  children: node.children!.map((c, k) => (k === i ? n : c)),
                })
              }
            />
            <button
              className="button button--small button--secondary"
              aria-label={`Usuń warunek ${i + 1}`}
              onClick={() =>
                update({
                  ...node,
                  children: node.children!.filter((_, k) => k !== i),
                })
              }
            >
              Usuń
            </button>
          </div>
        ))}
        <div className="action-row">
          <button
            className="button button--small button--secondary"
            disabled={node.children!.length >= 12}
            onClick={() =>
              update({
                ...node,
                children: [...node.children!, condition(fields)],
              })
            }
          >
            Dodaj warunek
          </button>
          <button
            className="button button--small button--secondary"
            disabled={depth >= 4 || node.children!.length >= 12}
            onClick={() =>
              update({
                ...node,
                children: [
                  ...node.children!,
                  { logic: "OR", children: [condition(fields)] },
                ],
              })
            }
          >
            Dodaj grupę OR
          </button>
        </div>
      </fieldset>
    );
  const field = fields.find((f) => f.key === node.field) ?? fields[0]!;
  const allowed =
    field.kind === "text"
      ? ["eq", "ne", "contains", "starts_with"]
      : field.kind === "boolean"
        ? ["eq"]
        : field.kind === "number" || field.kind === "time"
          ? ["eq", "ne", "gt", "gte", "lt", "lte"]
          : ["eq", "ne"];
  if (field.nullable) allowed.push("empty", "not_empty");
  return (
    <div className="filter-condition">
      <label>
        Pole
        <select
          aria-label="Pole"
          value={field.key}
          onChange={(e) =>
            update({ field: e.target.value, operator: "eq", value: "" })
          }
        >
          {fields.map((f) => (
            <option key={f.key} value={f.key}>
              {f.label}
            </option>
          ))}
        </select>
      </label>
      <label>
        Operator
        <select
          aria-label="Operator"
          value={node.operator}
          onChange={(e) =>
            update({
              ...node,
              operator: e.target.value,
              value: ["empty", "not_empty"].includes(e.target.value)
                ? ""
                : node.value,
            })
          }
        >
          {allowed.map((op) => (
            <option key={op} value={op}>
              {operators[op]}
            </option>
          ))}
        </select>
      </label>
      {!["empty", "not_empty"].includes(node.operator ?? "") &&
        (field.endpoint ? (
          <RemoteCombobox
            label="Wartość referencji"
            kind={field.key === "group_id" ? "groups" : "users"}
            endpoint={field.endpoint}
            value={node.value ? { id: node.value, label: node.value } : null}
            onChange={(c) => update({ ...node, value: c?.id ?? "" })}
          />
        ) : field.options || field.kind === "boolean" ? (
          <label>
            Wartość
            <select
              value={node.value ?? ""}
              onChange={(e) => update({ ...node, value: e.target.value })}
            >
              <option value="">Wybierz…</option>
              {(
                field.options ?? [
                  { value: "true", label: "Tak" },
                  { value: "false", label: "Nie" },
                ]
              ).map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          </label>
        ) : (
          <label>
            Wartość
            {field.kind === "time" && (
              <small>RFC 3339, np. 2026-10-13T12:00:00Z</small>
            )}
            <input
              maxLength={200}
              type={field.kind === "number" ? "number" : "text"}
              min={field.kind === "number" ? 1 : undefined}
              max={field.kind === "number" ? 5 : undefined}
              value={node.value ?? ""}
              onChange={(e) => update({ ...node, value: e.target.value })}
            />
          </label>
        ))}
    </div>
  );
}
export function filterLabel(n: FilterNode, fields: FilterField[]): string {
  if (n.logic)
    return `(${n.children?.map((c) => filterLabel(c, fields)).join(` ${n.logic} `)})`;
  const f = fields.find((f) => f.key === n.field);
  return `${f?.label ?? n.field} ${operators[n.operator ?? ""] ?? n.operator} ${f?.options?.find((o) => o.value === n.value)?.label ?? (n.value === "true" ? "Tak" : n.value === "false" ? "Nie" : (n.value ?? ""))}`;
}
