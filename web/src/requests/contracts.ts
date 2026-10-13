export type Condition = { field: string; equals: string | number | boolean };
export type Field = {
  key: string;
  label: string;
  type: "text" | "longtext" | "number" | "date" | "checkbox" | "select";
  required: boolean;
  options?: string[];
  visible_when?: Condition;
  required_when?: Condition;
};
export type Definition = {
  title: string;
  description: string;
  fields: Field[];
  method: string;
  group_id: string;
  audience_group_id?: string;
  steps: { title: string; instructions: string; required: boolean }[];
  workflow_id?: string;
  workflow_version?: string;
};
export type Item = {
  id: string;
  category_id: string;
  version: string;
  published_version?: string;
  draft: Definition;
};
export type Publication = {
  id: string;
  category_id: string;
  version: string;
  title: string;
  description: string;
  fields: Field[];
};
export type Node = {
  id: string;
  catalog_id?: string;
  name: string;
  description: string;
  version: string;
};
export type Values = Record<string, string | number | boolean>;
export type Request = {
  id: string;
  number: string;
  requester_id: string;
  requester_name: string;
  state: string;
  reason: string;
  version: string;
  created_at: string;
  updated_at: string;
  actions: string[];
  items: {
    id: string;
    number: string;
    snapshot: Publication;
    values: Values;
    total_tasks: number;
    completed_tasks: number;
    required_tasks: number;
    required_completed: number;
  }[];
};
export type Task = {
  id: string;
  request_id: string;
  requested_item_id: string;
  number: string;
  request_number: string;
  title: string;
  instructions: string;
  required: boolean;
  state: string;
  result: string;
  version: string;
  updated_at: string;
  assignment: {
    assigned_to?: string;
    assignment_group_id?: string;
    version: string;
  };
  assigned_name: string;
  group_name: string;
  actions: string[];
};
export const states: Record<string, string> = {
  SUBMITTED: "Złożony",
  IN_PROGRESS: "W realizacji",
  FULFILLED: "Zrealizowany",
  CLOSED: "Zamknięty",
  CANCELLED: "Anulowany",
  OPEN: "Nowe",
  ON_HOLD: "Wstrzymane",
  COMPLETED: "Ukończone",
};
export const message = (e: unknown) =>
  e instanceof Error ? e.message : "Nie udało się wykonać operacji.";
export function visible(field: Field, values: Values) {
  return (
    !field.visible_when ||
    values[field.visible_when.field] === field.visible_when.equals
  );
}
export function cleanValues(fields: Field[], values: Values) {
  const out: Values = {};
  for (const f of fields)
    if (visible(f, out) && Object.hasOwn(values, f.key))
      out[f.key] = values[f.key]!;
  return out;
}
