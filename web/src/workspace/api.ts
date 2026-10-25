import { api, jsonBody } from "../api/client";
import type {
  ActionResult,
  ExportStatus,
  FieldDefinition,
  QueuePage,
  SavedView,
  WorkItem,
  WorkQuery,
} from "./contracts";

export function queryWork(query: WorkQuery, signal?: AbortSignal) {
  return api<QueuePage>("/api/v1/workspace/queue", {
    method: "POST",
    body: jsonBody(query),
    signal,
  });
}

export function listWorkFields(signal?: AbortSignal) {
  return api<{ fields: FieldDefinition[]; types: { key: string; label: string }[] }>(
    "/api/v1/workspace/fields",
    { signal },
  );
}

export function listSavedViews(signal?: AbortSignal) {
  return api<{ items: SavedView[] }>("/api/v1/workspace/views", { signal });
}

export function saveWorkView(input: {
  id?: string;
  name: string;
  shared: boolean;
  version?: string;
  definition: WorkQuery;
}) {
  return api<SavedView>(
    input.version
      ? `/api/v1/workspace/views/${input.id}`
      : "/api/v1/workspace/views",
    {
      method: input.version ? "PUT" : "POST",
      body: jsonBody(input),
    },
  );
}

export function getWorkItem(type: string, id: string, signal?: AbortSignal) {
  return api<WorkItem>(`/api/v1/workspace/items/${encodeURIComponent(type)}/${encodeURIComponent(id)}`, { signal });
}

export function runWorkAction(input: {
  action: "assign" | "transition";
  items: {
    tenant_id: string;
    object_type: string;
    object_id: string;
    version: string;
    assignment_group_id?: string;
  }[];
  assigned_to?: string;
  state?: string;
  reason?: string;
  resolution_code?: string;
  resolution_description?: string;
}) {
  return api<{ results: ActionResult[] }>("/api/v1/workspace/actions", {
    method: "POST",
    body: jsonBody(input),
  });
}

export function requestWorkExport(query: WorkQuery, key: string) {
  return api<ExportStatus>("/api/v1/workspace/exports", {
    method: "POST",
    headers: { "Idempotency-Key": key },
    body: jsonBody(query),
  });
}

export function getWorkExport(id: string, signal?: AbortSignal) {
  return api<ExportStatus>(`/api/v1/workspace/exports/${id}`, { signal });
}

export function deleteWorkView(id: string, version: string) {
  return api<void>(`/api/v1/workspace/views/${id}?version=${version}`, {
    method: "DELETE",
  });
}
