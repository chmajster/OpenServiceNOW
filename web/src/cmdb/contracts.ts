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
  support_group_id?: string;
  support_group_name: string;
  criticality: string;
  lifecycle: string;
  attributes: Record<string, string | number>;
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
  max_length?: number;
  min: number;
  max: number;
  options?: string[];
};
export type Class = { key: string; label: string; attributes: Attribute[] };
export type Metadata = {
  classes: Class[];
  ci_import_fields: string[];
  asset_import_fields: string[];
  max_csv_bytes: number;
  max_csv_rows: number;
  max_csv_columns: number;
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
export const labels: Record<string, string> = {
  PHYSICAL_SERVER: "Serwer fizyczny",
  VM: "Maszyna wirtualna",
  NETWORK_DEVICE: "Urządzenie sieciowe",
  APPLICATION: "Aplikacja",
  SERVICE: "Usługa biznesowa",
  DATABASE: "Baza danych",
  LOW: "Niska",
  DESIGN: "Projektowanie",
  BUILD: "Budowa",
  OPERATE: "Eksploatacja",
  RETIRE: "Wycofywanie",
  PART_OF: "Jest częścią",
  newest: "Najnowsze",
  oldest: "Najstarsze",
  title: "Nazwa A–Z",
  updated: "Ostatnia aktualizacja",
  PLANNED: "Planowany",
  ACTIVE: "Aktywny",
  MAINTENANCE: "Konserwacja",
  RETIRED: "Wycofany",
  DEV: "Deweloperskie",
  TEST: "Testowe",
  STAGING: "Przedprodukcyjne",
  PROD: "Produkcyjne",
  ORDERED: "Zamówiony",
  IN_STOCK: "W magazynie",
  IN_USE: "W użyciu",
  IN_REPAIR: "W naprawie",
  DISPOSED: "Zutylizowany",
  RUNS_ON: "Działa na",
  HOSTED_ON: "Hostowana na",
  DEPENDS_ON: "Zależy od",
  CONNECTS_TO: "Łączy się z",
  CRITICAL: "Krytyczna",
  HIGH: "Wysoka",
  NORMAL: "Normalna",
  QUEUED_PREVIEW: "Podgląd w kolejce",
  PREVIEW_READY: "Podgląd gotowy",
  QUEUED_APPLY: "Zapis w kolejce",
  APPLYING: "Zapisywanie",
  COMPLETED: "Ukończony",
  PARTIAL: "Częściowo ukończony",
  FAILED: "Niepowodzenie",
  CANCELLED: "Anulowany",
  PENDING: "Oczekuje",
  APPLIED: "Zastosowano",
  ERROR: "Błąd",
  CONFLICT: "Konflikt",
  CREATE: "Utwórz",
  UPDATE: "Aktualizuj",
  NO_CHANGE: "Bez zmian",
};
export const text = (e: unknown) =>
  e instanceof Error ? e.message : "Nie udało się wykonać operacji";
