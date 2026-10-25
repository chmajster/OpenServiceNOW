import { act } from "react";
import { createRoot } from "react-dom/client";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { AuthProvider } from "../auth/AuthContext";
import { CMDBIREPage } from "./CMDBIREPage";

afterEach(() => {
  vi.unstubAllGlobals();
  window.localStorage.clear();
  document.documentElement.lang = "pl";
});

function response(value: unknown) {
  return new Response(JSON.stringify(value), { headers: { "Content-Type": "application/json" } });
}

function session(permissions: string[]) {
  return {
    user: { id: "7", email: "ala@example.test", display_name: "Ala Kowalska", version: "1", credential_version: "1" },
    tenant: { id: "2", name: "Firma", slug: "firma" }, permissions,
    password_change_required: false, expires_at: new Date(Date.now() + 600_000).toISOString(),
  };
}

function source(id: string, key: string, type: string, system = false) {
  return { id, source_key: key, name: key, source_type: type, owner_id: "7", scope_key: "default", state: "ACTIVE", priority: 10, is_system: system, version: "1" };
}

function rule() {
  return { class: "APPLICATION", version: "1", match_keys: [["identifier"]], required_attributes: [], normalization: { identifier: "LOWER_TRIM" }, parent_dependent: false, state: "ACTIVE" };
}

async function mount(permissionList: string[]) {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const calls: { path: string; method: string; body?: string }[] = [];
  vi.stubGlobal("fetch", vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
    const path = String(input);
    const method = (init?.method ?? "GET").toUpperCase();
    calls.push({ path, method, body: typeof init?.body === "string" ? init.body : undefined });
    if (path === "/api/v1/setup") return response({ required: false });
    if (path === "/api/v1/auth/me") return response(session(permissionList));
    if (path === "/api/v1/cmdb/ire/sources?limit=100") return response({ items: [source("10", "inventory_csv", "CSV"), source("1", "manual", "MANUAL", true)] });
    if (path === "/api/v1/cmdb/ire/rules") return response([rule()]);
    if (path === "/api/v1/cmdb/ire/policies") return response([]);
    if (path === "/api/v1/cmdb/ire/runs?limit=100") return response({ items: [] });
    if (path === "/api/v1/cmdb/ire/conflicts?state=OPEN&limit=100") return response({ items: [] });
    if (path === "/api/v1/cmdb/ire/quality/config") return response({ enabled: true, schedule_hours: 24, stale_days: 90, required_fields: { APPLICATION: ["identifier", "name"] }, version: "1", next_run_at: "2026-10-24T00:00:00Z" });
    if (path === "/api/v1/cmdb/ire/quality/summary") return response([{ rule_key: "required_fields", valid: 8, denominator: 10, open: 2 }]);
    if (path === "/api/v1/cmdb/classes") return response({ classes: [{ key: "APPLICATION", label: "Application", attributes: [] }] });
    if (path.startsWith("/api/v1/cmdb/ire/quality/findings")) return response({ items: [] });
    if (path === "/api/v1/cmdb/ire/match" && method === "POST") return response({ state: "SINGLE_MATCH", ci_id: "73", candidate_ci_ids: ["73"], rule_version: "1" });
    return response({});
  }));
  const container = document.createElement("div");
  const root = createRoot(container);
  await act(async () => {
    root.render(<MemoryRouter initialEntries={["/cmdb/ire"]}><AuthProvider><Routes><Route path="/cmdb/ire" element={<CMDBIREPage />} /></Routes></AuthProvider></MemoryRouter>);
  });
  await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
  return { calls, container, root };
}

it("loads IRE administration and tests one observation through the match API", async () => {
  const { calls, container, root } = await mount(["cmdb.ire.manage", "cmdb.manage", "cmdb.read", "cmdb.quality.read"]);
  try {
    expect(container.textContent).toContain("inventory_csv");
    const matchTab = [...container.querySelectorAll("[role=tab]")].find((item) => /match|tester|dopasowania/i.test(item.textContent ?? ""));
    expect(matchTab).toBeTruthy();
    await act(async () => { matchTab!.dispatchEvent(new MouseEvent("click", { bubbles: true })); });
    const textarea = container.querySelector("textarea") as HTMLTextAreaElement;
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!.call(textarea, JSON.stringify({
      source_native_key: "node-9",
      values: { class: "APPLICATION", identifier: "app-9", name: "App 9", status: "ACTIVE", owner_id: "7", environment: "PROD" },
    }));
    await act(async () => { textarea.dispatchEvent(new Event("input", { bubbles: true })); });
    const form = container.querySelector("form");
    expect(form).toBeTruthy();
    await act(async () => { form!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })); });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
    const request = calls.find((call) => call.path === "/api/v1/cmdb/ire/match" && call.method === "POST");
    expect(request).toBeTruthy();
    expect(JSON.parse(request!.body!).observation.source_native_key).toBe("node-9");
    expect(container.textContent).toContain("SINGLE_MATCH");
  } finally {
    await act(async () => root.unmount());
  }
});

it("shows quality metrics to a read-only quality user without loading management APIs", async () => {
  const { calls, container, root } = await mount(["cmdb.quality.read", "cmdb.read"]);
  try {
    expect(container.textContent).toMatch(/Jakość danych|Data quality/);
    expect(container.textContent).toContain("required_fields");
    expect(container.textContent).toMatch(/Możesz przeglądać wyniki jakości|You can view quality results/);
    expect(calls.some((call) => call.path.includes("/ire/sources"))).toBe(false);
    expect(calls.some((call) => call.path.includes("/ire/quality/summary"))).toBe(true);
    expect(container.textContent).toContain("Application");
    const filters = [...container.querySelectorAll("form")].find((form) => form.querySelector("select"));
    expect(filters).toBeTruthy();
    const selects = filters!.querySelectorAll("select");
    const classSelect = selects[0]!;
    Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, "value")!.set!.call(classSelect, "APPLICATION");
    await act(async () => { classSelect.dispatchEvent(new Event("change", { bubbles: true })); });
    const submit = [...filters!.querySelectorAll("button")].find((button) => /Filter|Filtruj/i.test(button.textContent ?? ""));
    expect(submit).toBeTruthy();
    await act(async () => { filters!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })); });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
    expect(calls.some((call) => call.path.includes("class=APPLICATION"))).toBe(true);
  } finally {
    await act(async () => root.unmount());
  }
});
