export type PortfolioItem = {
  id: string;
  service_key: string;
  name: string;
  type: "BUSINESS_APPLICATION" | "SERVICE_INSTANCE" | "TECHNICAL_SERVICE" | "BUSINESS_SERVICE";
  description: string;
  lifecycle: "DRAFT" | "PUBLISHED" | "ACTIVE" | "RETIRED";
  criticality: "CRITICAL" | "HIGH" | "MEDIUM" | "LOW";
  environment: "DEV" | "TEST" | "STAGING" | "PROD" | "ALL";
  scope: string;
  business_owner_id: string;
  business_owner_name: string;
  technical_owner_id: string;
  technical_owner_name: string;
  support_group_id: string;
  support_group_name: string;
  org_unit_ref_id: string;
  org_unit_name: string;
  department_ref_id: string;
  department_name: string;
  cost_center_ref_id: string;
  cost_center_name: string;
  location_id: string;
  location_name: string;
  ci_id: string;
  ci_name: string;
  ci_class: string;
  ci_version: string;
  dependency_mode: "NOT_MODELED" | "ALL" | "ANY" | "QUORUM";
  dependency_quorum: number;
  version: string;
};

export type PortfolioRelation = {
  id: string;
  version: string;
  source_item_id: string;
  target_item_id: string;
  target_ci_id: string;
  target_version: string;
  type: "REALIZES" | "DEPENDS_ON" | "MAPS_TO_CI";
  source_name: string;
  source_type: string;
  target_name: string;
  target_type?: string;
  target_ci_class?: string;
};

export type PortfolioOffer = {
  id: string;
  offer_key: string;
  parent_service_id: string;
  parent_service_name: string;
  catalog_item_id: string;
  catalog_item_title?: string;
  name: string;
  description: string;
  availability_conditions: string;
  support_model: "STANDARD" | "EXTENDED" | "PREMIUM" | "CUSTOM";
  obligation_summary: string;
  sla_definition_id: string;
  audience_group_ids?: string[];
  lifecycle: "DRAFT" | "ACTIVE" | "WITHDRAWN";
  version: string;
};

export type PublicOffer = Omit<PortfolioOffer, "parent_service_id" | "parent_service_name" | "lifecycle" | "audience_group_ids"> & {
  service_name: string;
};

export type LookupOption = {
  id: string;
  name?: string;
  email?: string;
  identifier?: string;
  service_key?: string;
  key?: string;
  type?: string;
  class?: string;
  environment?: string;
  version?: string;
  lifecycle?: string;
  active?: boolean;
};

export type ImpactNode = {
  id: string;
  kind: string;
  name: string;
  class?: string;
  lifecycle?: string;
  environment?: string;
  depth: number;
  availability?: string;
  availability_rule?: string;
};

export type ImpactResult = {
  root: ImpactNode;
  nodes: ImpactNode[];
  edges: { from: string; to: string; type: string }[];
  impacted_services: { id: string; name: string; type: string; path: string[]; rule: string }[];
  impacted_offers: { id: string; name: string; type: string; path: string[]; rule: string }[];
  rules_used: string[];
  truncated: boolean;
  max_nodes: number;
  max_depth: number;
  rule_version: string;
  calculated_at: string;
};

export type Availability = {
  node_kind: "ITEM" | "CI";
  node_id: string;
  state: "AVAILABLE" | "DEGRADED" | "UNAVAILABLE" | "UNKNOWN" | "NOT_MODELED";
  source: string;
  note: string;
  version: string;
};

export type RetirementImpact = {
  service_id: string;
  active_offers: number;
  active_catalog_items: number;
  open_incidents: number;
  incidents_visible: boolean;
  open_requests: number;
  requests_visible: boolean;
  dependent_items: number;
  retirement_allowed: boolean;
};

export type Page<T> = { items: T[]; next_before_id?: string };
