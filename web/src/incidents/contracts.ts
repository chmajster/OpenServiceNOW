import type { IncidentTemplate } from "./majorContracts";
import type { Choice } from "../components/RemoteCombobox";
import type { MetadataSchema } from "../metadata/contracts";
import { formatDateTime } from "../metadata/format";
export type Option = { retired?:boolean; value: string; label: string; labels?: Partial<Record<"pl" | "en", string>> };
export type Metadata = {
 templates?:IncidentTemplate[];
  categories: (Option & { subcategories: Option[] })[];
  states: Option[];
  resolution_codes: Option[];
  hold_reasons: Option[];
  priority_matrix: number[][];
  can_create: boolean;
  custom_fields_schema: MetadataSchema;
};
export type Assignment = {
  assigned_to?: string;
  assignment_group_id?: string;
  version: string;
};
export type IncidentSummary = {
  id: string;
  number: string;
  title: string;
  state: string;
  priority: number;
  reporter_id: string;
  reporter_name: string;
  affected_user_id: string;
  affected_user_name: string;
  category: string;
  subcategory: string;
  impact: number;
  urgency: number;
  assignment: Assignment;
  assigned_name: string;
  group_name: string;
  version: string;
  custom_fields?: Record<string, unknown>;
  custom_fields_version: string;
  created_at: string;
  updated_at: string;
};
export type Incident = IncidentSummary & {
 service_id?:string|null; location?:string; template_key?:string;template_version?:string;assignment_origin?:string;assignment_reason?:string;
  description: string;
  subcategory: string;
  impact: number;
  urgency: number;
  calculated_priority: number;
  priority_override: number | null;
  priority_reason: string;
  hold_reason: string;
  resolution_code: string;
  resolution_description: string;
  cancellation_reason: string;
  started_at: string | null;
  resolved_at: string | null;
  closed_at: string | null;
  cancelled_at: string | null;
  actions: {
    edit: boolean;
    assign: boolean;
    override_priority: boolean;
    transitions: string[];
  };
};
export type IncidentPage = {
  incidents: IncidentSummary[];
  next_after_id?: string;
  next_after_priority?: string;
};
export type FieldDraft = {
 service_id?:string;location?:string;template_key?:string;template_version?:string;
  title: string;
  description: string;
  affected: Choice | null;
  category: string;
  subcategory: string;
  impact: string;
  urgency: string;
  custom_fields: Record<string, unknown>;
  custom_fields_version: string;
};
export function draftOf(record: Incident): FieldDraft {
  return {
    service_id:record.service_id??"",location:record.location??"",template_key:record.template_key??"",template_version:record.template_version??"0",
 title: record.title,
    description: record.description,
    affected: { id: record.affected_user_id, label: record.affected_user_name },
    category: record.category,
    subcategory: record.subcategory,
    impact: String(record.impact),
    urgency: String(record.urgency),
    custom_fields: record.custom_fields ?? {},
    custom_fields_version: record.custom_fields_version,
  };
}
export function fieldsOf(draft: FieldDraft) {
  const title = draft.title.trim(),
    description = draft.description.trim();
  if (Array.from(title).length < 1 || Array.from(title).length > 200)
    throw new Error("Tytuł wymaga 1–200 znaków.");
  if (
    Array.from(description).length < 1 ||
    Array.from(description).length > 20000
  )
    throw new Error("Opis wymaga 1–20000 znaków.");
  if (!draft.affected) throw new Error("Wybierz osobę dotkniętą problemem.");
  return {
    service_id:draft.service_id||null,location:draft.location??"",template_key:draft.template_key??"",template_version:draft.template_version??"0",
 title,
    description,
    affected_user_id: draft.affected.id,
    category: draft.category,
    subcategory: draft.subcategory,
    impact: Number(draft.impact),
    urgency: Number(draft.urgency),
    custom_fields: draft.custom_fields,
    custom_fields_version: draft.custom_fields_version,
  };
}
export function calculatedPriority(
  metadata: Metadata,
  impact: string,
  urgency: string,
) {
  return metadata.priority_matrix[Number(impact) - 1]?.[Number(urgency) - 1];
}
export const dateLabel = (value: string | null) =>
  formatDateTime(value, typeof document !== "undefined" && document.documentElement.lang === "en" ? "en" : "pl");
export const optionLabel = (options: Option[], value: string) => {
  const option = options.find((o) => o.value === value);
  const locale = typeof document !== "undefined" && document.documentElement.lang === "en" ? "en" : "pl";
  return option?.labels?.[locale] || option?.label || value;
};
export type HistoryEntry = {
  id: string;
  type: string;
  summary: string;
  actor_user_id?: string;
  actor_name?: string;
  source_event_id?: string;
  event_time: string;
  data: {
    actor_user_id: string;
    incident_version: string;
    occurred_at: string;
    before?: Record<string, unknown> | null;
    after?: Record<string, unknown>;
    body?: string;
    file_name?: string;
    reason?: string;
  };
};
export type HistoryPage = {
  entries: HistoryEntry[];
  next_before_event_id?: string;
  pending: boolean;
};
