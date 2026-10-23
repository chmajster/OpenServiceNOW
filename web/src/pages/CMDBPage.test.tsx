import { act } from "react";
import { createRoot } from "react-dom/client";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { AuthProvider } from "../auth/AuthContext";
import { CMDBCreatePage, CMDBListPage } from "./CMDBPage";

afterEach(() => {
  vi.unstubAllGlobals();
  window.localStorage.clear();
  document.documentElement.lang = "pl";
});

it("loads the authorized CMDB inventory with server filters and persists search in the URL", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const requests: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string | URL | Request) => {
      const path = String(input);
      requests.push(path);
      const payload = path === "/api/v1/setup"
        ? { required: false }
        : path === "/api/v1/auth/me"
          ? {
              user: {
                id: "7",
                email: "ala@example.test",
                display_name: "Ala Kowalska",
                version: "1",
                credential_version: "1",
              },
              tenant: { id: "2", name: "Firma", slug: "firma" },
              permissions: ["cmdb.read", "cmdb.manage"],
              password_change_required: false,
              expires_at: new Date(Date.now() + 600_000).toISOString(),
            }
          : path === "/api/v1/cmdb/classes"
            ? {
                classes: [
                  { key: "VM", label: "Maszyna wirtualna", attributes: [] },
                  { key: "SERVICE", label: "Usługa", attributes: [] },
                ],
                ci_fields: [],
                statuses: ["ACTIVE", "MAINTENANCE"],
                environments: ["PROD", "TEST"],
                relation_types: [],
                ci_import_fields: [],
                ci_import_required_fields: [],
                ci_import_identity_keys: [],
                asset_import_fields: [],
                asset_import_required_fields: [],
                asset_import_identity_keys: [],
                max_csv_bytes: 1_048_576,
                max_csv_rows: 500,
                max_csv_columns: 40,
              }
            : {
                items: [
                  {
                    id: "72",
                    version: "1",
                    identifier: "vpn-gw-ci",
                    name: "VPN gateway CI",
                    class: "VM",
                    status: "ACTIVE",
                    owner_id: "7",
                    owner_name: "Ala Kowalska",
                    environment: "PROD",
                    location: "Warszawa",
                    attributes: {},
                    archived: false,
                    created_at: "2026-01-01T00:00:00Z",
                    updated_at: "2026-01-01T00:00:00Z",
                  },
                ],
                next_before_id: "",
              };
      return new Response(JSON.stringify(payload), {
        headers: { "Content-Type": "application/json" },
      });
    }),
  );

  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={["/cmdb"]}>
          <AuthProvider>
            <Routes>
              <Route path="/cmdb" element={<CMDBListPage />} />
            </Routes>
          </AuthProvider>
        </MemoryRouter>,
      );
    });
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    expect(container.textContent).toContain("VPN gateway CI");
    expect(container.textContent).toContain("vpn-gw-ci");
    expect(requests).toContain("/api/v1/cmdb/cis?archived=false&limit=25");
    expect(container.querySelector('a[href="/cmdb/72?return=%2Fcmdb"]')).not.toBeNull();
    const filterForm = container.querySelector("form");
    const search = filterForm?.querySelector("input") as HTMLInputElement | null;
    expect(filterForm).not.toBeNull();
    expect(search).not.toBeNull();

    const setValue = Object.getOwnPropertyDescriptor(
      HTMLInputElement.prototype,
      "value",
    )?.set;
    await act(async () => {
      setValue?.call(search, "VPN");
      search?.dispatchEvent(new Event("input", { bubbles: true }));
      filterForm?.dispatchEvent(
        new Event("submit", { bubbles: true, cancelable: true }),
      );
    });
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    expect(requests).toContain("/api/v1/cmdb/cis?q=VPN&archived=false&limit=25");
    expect(container.querySelector('a[href*="return=%2Fcmdb%3Fq%3DVPN"]')).not.toBeNull();
  } finally {
    await act(async () => root.unmount());
  }
});

it("creates a workstation CI with its class-specific fields", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  let submitted: Record<string, unknown> | undefined;
  const jsonResponse = (payload: unknown, status = 200) => new Response(JSON.stringify(payload), {
    status,
    headers: { "Content-Type": "application/json" },
  });
  const classMetadata = {
    classes: [{
      key: "WORKSTATION",
      label: "Workstation",
      attributes: [
        { key: "form_factor", label: "Computer type", type: "SELECT", required: false, min: 0, max: 0, options: ["DESKTOP", "LAPTOP"] },
        { key: "mac_address", label: "MAC address", type: "MAC", required: false, min: 0, max: 0, max_length: 17 },
      ],
    }],
    ci_fields: [
      { key: "identifier", label: "Identifier", type: "TEXT", required: true, max_length: 100, min: 0, max: 0 },
      { key: "name", label: "Name", type: "TEXT", required: true, max_length: 160, min: 0, max: 0 },
      { key: "class", label: "Class", type: "SELECT", required: true, min: 0, max: 0, options: ["WORKSTATION"] },
      { key: "status", label: "Status", type: "SELECT", required: true, min: 0, max: 0, options: ["ACTIVE"] },
      { key: "owner_id", label: "Owner", type: "USER_REFERENCE", required: true, min: 0, max: 0 },
      { key: "environment", label: "Environment", type: "SELECT", required: true, min: 0, max: 0, options: ["PROD"] },
      { key: "location", label: "Location", type: "TEXT", required: false, max_length: 160, min: 0, max: 0 },
    ],
    statuses: ["ACTIVE"],
    environments: ["PROD"],
    relation_types: [],
    ci_import_fields: [],
    ci_import_required_fields: [],
    ci_import_identity_keys: [],
    asset_import_fields: [],
    asset_import_required_fields: [],
    asset_import_identity_keys: [],
    max_csv_bytes: 1_048_576,
    max_csv_rows: 500,
    max_csv_columns: 40,
    custom_fields_schema: {
      type_key: "ci",
      version: "1",
      fields: [],
      form: { sections: [], tabs: [] },
      list: { columns: ["identifier", "name", "class", "status"], default_sort: "newest", search_fields: [], filters: [] },
    },
  };
  vi.stubGlobal("fetch", vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
    const path = new URL(String(input), window.location.origin).pathname;
    if (path === "/api/v1/setup") return jsonResponse({ required: false });
    if (path === "/api/v1/auth/me") return jsonResponse({
      user: { id: "7", email: "ala@example.test", display_name: "Ala Kowalska", version: "1", credential_version: "1" },
      tenant: { id: "2", name: "Firma", slug: "firma" },
      permissions: ["cmdb.read", "cmdb.manage"],
      password_change_required: false,
      expires_at: new Date(Date.now() + 600_000).toISOString(),
    });
    if (path === "/api/v1/cmdb/classes") return jsonResponse(classMetadata);
    if (path === "/api/v1/cmdb/cis" && init?.method === "POST") {
      submitted = JSON.parse(String(init.body)) as Record<string, unknown>;
      return jsonResponse({ ...submitted, id: "73", version: "1", owner_name: "Ala Kowalska", archived: false, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z" }, 201);
    }
    return jsonResponse({ error: { code: "NOT_FOUND", message: "Not found" } }, 404);
  }));

  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={["/cmdb/new"]}>
          <AuthProvider>
            <Routes><Route path="/cmdb/new" element={<CMDBCreatePage />} /></Routes>
          </AuthProvider>
        </MemoryRouter>,
      );
    });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
    expect(container.textContent).toContain("Komputer użytkownika");
    expect(container.textContent).toContain("Adres MAC");

    const setInputValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
    const setSelectValue = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, "value")?.set;
    const input = (label: string) => [...container.querySelectorAll("label")].find((item) => item.textContent?.startsWith(label))?.querySelector("input");
    const values: Array<[string, string]> = [["Identyfikator", "ws-1"], ["Nazwa", "Laptop 1"], ["Adres MAC", "00:11:22:33:44:55"]];
    for (const [label, value] of values) {
      const element = input(label);
      expect(element).not.toBeNull();
      await act(async () => {
        setInputValue?.call(element, value);
        element?.dispatchEvent(new Event("input", { bubbles: true }));
      });
    }
    const formFactor = [...container.querySelectorAll("select")].find((element) => [...element.options].some((option) => option.value === "LAPTOP"));
    expect(formFactor).not.toBeNull();
    await act(async () => {
      setSelectValue?.call(formFactor, "LAPTOP");
      formFactor?.dispatchEvent(new Event("change", { bubbles: true }));
    });
    const form = container.querySelector("form");
    expect(form).not.toBeNull();
    await act(async () => {
      form?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    expect(submitted).toMatchObject({
      identifier: "ws-1",
      name: "Laptop 1",
      class: "WORKSTATION",
      owner_id: "7",
      attributes: { form_factor: "LAPTOP", mac_address: "00:11:22:33:44:55" },
    });
  } finally {
    await act(async () => root.unmount());
  }
});
