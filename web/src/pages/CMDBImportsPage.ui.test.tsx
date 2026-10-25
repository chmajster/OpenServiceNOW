import { act } from "react";
import { createRoot } from "react-dom/client";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { AuthProvider } from "../auth/AuthContext";
import { CMDBImportsPage } from "./CMDBImportsPage";

afterEach(() => {
  vi.unstubAllGlobals();
  window.localStorage.clear();
  document.documentElement.lang = "pl";
});

it("sends the selected active CSV source with a CI import", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  let submittedConfig: Record<string, unknown> | undefined;
  const calls: string[] = [];
  const response = (payload: unknown) => new Response(JSON.stringify(payload), { headers: { "Content-Type": "application/json" } });
  const metadata = {
    classes: [{ key: "APPLICATION", label: "Application", attributes: [] }],
    ci_fields: [], statuses: ["ACTIVE"], environments: ["PROD"], relation_types: [],
    ci_import_fields: ["identifier", "name", "class", "status", "owner_id", "environment"],
    ci_import_required_fields: ["identifier", "name", "class", "status", "owner_id", "environment"],
    ci_import_identity_keys: ["identifier"], asset_import_fields: [], asset_import_required_fields: [], asset_import_identity_keys: [],
    max_csv_bytes: 1_048_576, max_csv_rows: 500, max_csv_columns: 40,
    custom_fields_schema: { type_key: "ci", version: "1", fields: [], form: { sections: [], tabs: [] }, list: { columns: [], default_sort: "newest", search_fields: [], filters: [] } },
  };
  vi.stubGlobal("fetch", vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
    const url = new URL(String(input), window.location.origin);
    const path = url.pathname;
    const method = init?.method ?? "GET";
    calls.push(path);
    if (path === "/api/v1/setup") return response({ required: false });
    if (path === "/api/v1/auth/me") return response({
      user: { id: "7", email: "ala@example.test", display_name: "Ala Kowalska", version: "1", credential_version: "1" },
      tenant: { id: "2", name: "Firma", slug: "firma" },
      permissions: ["cmdb.read", "cmdb.manage", "cmdb.import", "cmdb.ire.manage"],
      password_change_required: false, expires_at: new Date(Date.now() + 600_000).toISOString(),
    });
    if (path === "/api/v1/cmdb/classes") return response(metadata);
    if (path === "/api/v1/cmdb/imports" && method === "GET") return response({ items: [], next_before_id: "" });
    if (path === "/api/v1/cmdb/ire/sources") return response({ items: [
      { id: "42", name: "Finance discovery", source_type: "CSV", state: "ACTIVE" },
      { id: "43", name: "Disabled source", source_type: "CSV", state: "DISABLED" },
      { id: "1", name: "Manual", source_type: "MANUAL", state: "ACTIVE" },
    ] });
    if (path === "/api/v1/cmdb/imports" && method === "POST") {
      const body = init?.body as FormData;
      submittedConfig = JSON.parse(String(body.get("config"))) as Record<string, unknown>;
      return response({ id: "90", target: "CI", identity_key: "identifier", mapping: {}, creator_id: "7", executor_id: "7", file_name: "inventory.csv", state: "QUEUED_PREVIEW", error_code: "", version: "1", job_status: "QUEUED", progress: 0, attempts: 0, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z", counts: {} });
    }
    return response({});
  }));

  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () => {
      root.render(<MemoryRouter initialEntries={["/cmdb/imports"]}><AuthProvider><Routes><Route path="/cmdb/imports" element={<CMDBImportsPage />} /></Routes></AuthProvider></MemoryRouter>);
    });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });

    expect(calls).toContain("/api/v1/cmdb/ire/sources");
    const sourceSelect = [...container.querySelectorAll("select")].find((select) => [...select.options].some((option) => option.value === "42")) as HTMLSelectElement;
    expect(sourceSelect).toBeTruthy();
    expect([...sourceSelect.options].map((option) => option.value)).toEqual(["", "42"]);
    Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, "value")!.set!.call(sourceSelect, "42");
    await act(async () => { sourceSelect.dispatchEvent(new Event("change", { bubbles: true })); });

    const fileInput = container.querySelector('input[type="file"]') as HTMLInputElement;
    Object.defineProperty(fileInput, "files", { configurable: true, value: [new File(["identifier,name,class,status,owner_id,environment\n"], "inventory.csv", { type: "text/csv" })] });
    await act(async () => { fileInput.dispatchEvent(new Event("change", { bubbles: true })); });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
    const form = container.querySelector("form")!;
    await act(async () => { form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })); });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });

    expect(submittedConfig?.source_id).toBe("42");
    expect(submittedConfig?.target).toBe("CI");
  } finally {
    await act(async () => root.unmount());
  }
});
