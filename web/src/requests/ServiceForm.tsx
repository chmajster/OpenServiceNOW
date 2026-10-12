import type { Field, Values } from "./contracts";
import { cleanValues, visible } from "./contracts";
export function ServiceForm({
  fields,
  values,
  onChange,
  disabled = false,
}: {
  fields: Field[];
  values: Values;
  onChange: (values: Values) => void;
  disabled?: boolean;
}) {
  const clean = cleanValues(fields, values);
  return (
    <div className="form-grid">
      {fields
        .filter((f) => visible(f, clean))
        .map((f) => {
          const required =
            f.required ||
            (!!f.required_when &&
              clean[f.required_when.field] === f.required_when.equals);
          const update = (value: string | number | boolean | undefined) => {
            const next = { ...values };
            if (value === undefined) delete next[f.key];
            else next[f.key] = value;
            onChange(cleanValues(fields, next));
          };
          return (
            <label key={f.key}>
              {f.label}
              {required ? " *" : ""}
              {f.type === "longtext" ? (
                <textarea
                  required={required}
                  disabled={disabled}
                  maxLength={10000}
                  value={String(values[f.key] ?? "")}
                  onChange={(e) => update(e.target.value)}
                />
              ) : f.type === "select" ? (
                <select
                  required={required}
                  disabled={disabled}
                  value={String(values[f.key] ?? "")}
                  onChange={(e) => update(e.target.value || undefined)}
                >
                  <option value="">Wybierz…</option>
                  {f.options?.map((o) => (
                    <option key={o}>{o}</option>
                  ))}
                </select>
              ) : f.type === "checkbox" ? (
                <input
                  type="checkbox"
                  required={required}
                  disabled={disabled}
                  checked={values[f.key] === true}
                  onChange={(e) => update(e.target.checked)}
                />
              ) : (
                <input
                  required={required}
                  disabled={disabled}
                  type={
                    f.type === "number"
                      ? "number"
                      : f.type === "date"
                        ? "date"
                        : "text"
                  }
                  min={f.type === "number" ? -1e12 : undefined}
                  max={f.type === "number" ? 1e12 : undefined}
                  step={f.type === "number" ? "any" : undefined}
                  maxLength={1000}
                  value={String(values[f.key] ?? "")}
                  onChange={(e) =>
                    update(
                      f.type === "number"
                        ? e.target.value === ""
                          ? undefined
                          : Number(e.target.value)
                        : e.target.value,
                    )
                  }
                />
              )}
            </label>
          );
        })}
    </div>
  );
}
