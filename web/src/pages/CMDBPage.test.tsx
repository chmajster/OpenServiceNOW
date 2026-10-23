import { act } from "react";
import { createRoot } from "react-dom/client";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { AuthProvider } from "../auth/AuthContext";
import { CMDBListPage } from "./CMDBPage";

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
