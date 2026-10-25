import type { Expression } from "./contracts";

const isEmpty = (value: unknown) => value === undefined || value === null || value === "" || Array.isArray(value) && value.length === 0;

export function evaluateRule(rule: Expression | undefined, values: Record<string, unknown>): boolean {
  if (!rule) return true;
  const op = rule.op.toUpperCase();
  if (op === "AND") return (rule.children ?? []).every((child) => evaluateRule(child, values));
  if (op === "OR") return (rule.children ?? []).some((child) => evaluateRule(child, values));
  const value = values[rule.field ?? ""];
  if (op === "IS_EMPTY") return isEmpty(value);
  if (op === "IS_NOT_EMPTY") return !isEmpty(value);
  if (op === "EQ") return JSON.stringify(value) === JSON.stringify(rule.value);
  if (op === "NE") return JSON.stringify(value) !== JSON.stringify(rule.value);
  if (op === "CONTAINS") return Array.isArray(value) ? value.includes(rule.value) : String(value ?? "").includes(String(rule.value ?? ""));
  const left = typeof value === "number" ? value : Date.parse(String(value ?? ""));
  const right = typeof rule.value === "number" ? rule.value : Date.parse(String(rule.value ?? ""));
  const numericLeft = typeof value === "number" ? value : Number(value);
  const numericRight = typeof rule.value === "number" ? rule.value : Number(rule.value);
  const a = Number.isFinite(left) ? left : numericLeft;
  const b = Number.isFinite(right) ? right : numericRight;
  if (!Number.isFinite(a) || !Number.isFinite(b)) return false;
  switch (op) {
    case "GT": return a > b;
    case "GTE": return a >= b;
    case "LT": return a < b;
    case "LTE": return a <= b;
    default: return false;
  }
}
