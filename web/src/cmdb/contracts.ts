import { cmdbLabel } from "./messages";
import type { MetadataSchema } from "../metadata/contracts";

export type CI = {
  id: string;
  version: string;
  identifier: string;
  name: string;
  class: string;
  status: string;
  owner_id: string;
  owner_name: string;
  environment: string;
  location: string;
  attributes: Record<string, unknown>;
  custom_fields?: Record<string, unknown>;
  custom_fields_version: string;
  archived: boolean;
  created_at: string;
  updated_at: string;
};

export type Asset = {
  id: string;
  version: string;
  asset_tag: string;
  name: string;
  owner_id: string;
  owner_name: string;
  serial_number: string;
  vendor: string;
  model: string;
  location: string;
  state: string;
  purchase_date: string;
  purchase_cost: string;
  currency: string;
  warranty_until: string;
  archived: boolean;
  created_at: string;
  updated_at: string;
};

export type Inventory = CI | Asset;

export type Attribute = {
  key: string;
  label: string;
  type: string;
  required: boolean;
  max_length?: number;
  min: number;
  max: number;
  options?: string[];
};

export type Class = {
  key: string;
  label: string;
  attributes: Attribute[];
};

export type RelationPair = { source: string; target: string };
export type RelationDefinition = { key: string; pairs: RelationPair[] };

export type Metadata = {
  classes: Class[];
  ci_fields: Attribute[];
  statuses: string[];
  environments: string[];
  relation_types: RelationDefinition[];
  ci_import_fields: string[];
  ci_import_required_fields: string[];
  ci_import_identity_keys: string[];
  asset_import_fields: string[];
  asset_import_required_fields: string[];
  asset_import_identity_keys: string[];
  max_csv_bytes: number;
  max_csv_rows: number;
  max_csv_columns: number;
  custom_fields_schema: MetadataSchema;
};

export type Relation = {
  id: string;
  version: string;
  source_id: string;
  target_id: string;
  source_name: string;
  target_name: string;
  source_class: string;
  target_class: string;
  type: string;
};

export type Graph = {
  nodes: {
    id: string;
    name: string;
    class: string;
    status: string;
    archived: boolean;
    depth: number;
  }[];
  edges: Relation[];
  truncated: boolean;
  max_nodes: number;
  max_depth: number;
  max_edges: number;
};

export type Reference = {
  id: string;
  number: string;
  title: string;
  state: string;
  version: string;
  can_link: boolean;
};

export type ImportConfig = {
  target: "CI" | "ASSET";
  identity_key: string;
  mapping: Record<string, string>;
  source_id?: string;
  custom_fields_version?: string;
  custom_fields?: string[];
};

export type Import = ImportConfig & {
  id: string;
  version: string;
  creator_id: string;
  executor_id: string;
  file_name: string;
  state: string;
  error_code: string;
  job_id?: string;
  job_status: string;
  progress: number;
  attempts: number;
  counts: Record<string, number>;
  created_at: string;
  updated_at: string;
};

export type ImportRow = {
  row_number: number;
  identity_value: string;
  action: string;
  state: string;
  record_id?: string;
  expected_version: string;
  before: unknown;
  after: unknown;
  error_code: string;
  error_message: string;
};

export const labels: Record<string, string> = new Proxy(
  {},
  {
    get: (_target, key) =>
      typeof key === "string" ? cmdbLabel(key) : undefined,
  },
);

export function fields(record: Inventory) {
  const {
    id,
    version,
    owner_name,
    archived,
    created_at,
    updated_at,
    ...values
  } = record;
  void id;
  void version;
  void owner_name;
  void archived;
  void created_at;
  void updated_at;
  return values;
}

export const text = (error: unknown) =>
  error instanceof Error ? error.message : "Nie udało się wykonać operacji";
