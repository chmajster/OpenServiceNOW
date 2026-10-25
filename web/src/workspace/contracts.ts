export type WorkReference = {
  tenant_id: string;
  object_type: "incident" | "fulfillment_task" | "incident_action";
  object_id: string;
};

export type WorkItem = WorkReference & {
  number: string;
  title: string;
  state: string;
  state_category: string;
  category: string;
  priority?: number;
  assigned_to?: string;
  assigned_name: string;
  assignment_group_id?: string;
  group_name: string;
  version: string;
  assignment_version: string;
  created_at: string;
  updated_at: string;
  due_at?: string;
  watched: boolean;
  record_path: string;
  capabilities: string[];
  search_rank?: number;
  custom_fields?: Record<string, string | number | boolean | string[] | null>;
};

export type FilterNode = {
  operator?: "AND" | "OR";
  children?: FilterNode[];
  field?: string;
  compare?: string;
  value?: string | number | boolean | string[] | number[];
};

export type WorkQuery = {
  types?: WorkReference["object_type"][];
  view: "mine" | "group" | "unassigned" | "watched" | "all";
  query?: string;
  filters?: FilterNode;
  sort: "updated_desc" | "created_desc" | "priority_asc" | "number_asc" | "rank";
  group_by?: string;
  columns?: string[];
  column_order?: string[];
  column_widths?: Record<string, number>;
  limit: number;
  cursor?: string;
};

export type QueuePage = { items: WorkItem[]; next_cursor?: string; limit: number };

export type FieldDefinition = {
  key: string;
  label: string;
  type: "text" | "enum" | "number" | "integer" | "decimal" | "reference" | "date" | "date_only" | "datetime" | "boolean";
  operators: string[];
  reference_type?: string;
  object_type?: string;
  filter_only?: boolean;
  options?: { value: string; label: string }[];
};

export type SavedView = {
  id: string;
  tenant_id: string;
  owner_user_id: string;
  name: string;
  shared: boolean;
  version: string;
  definition: WorkQuery;
  created_at: string;
  updated_at: string;
};

export type ActionResult = WorkReference & {
  status: "SUCCEEDED" | "CONFLICT" | "FORBIDDEN" | "NOT_FOUND" | "INVALID" | "INVALID_TRANSITION" | "FAILED";
  message?: string;
  item?: WorkItem;
};

export type ExportStatus = {
  job_id: string;
  status: string;
  progress: number;
  error?: string;
  download_path?: string;
  created_at: string;
  updated_at: string;
};
