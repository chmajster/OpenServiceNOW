import { act } from "react";
import { createRoot } from "react-dom/client";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { AuthProvider } from "../auth/AuthContext";
import { GlobalSearchPage } from "./GlobalSearchPage";

afterEach(() => vi.unstubAllGlobals());

it("searches permitted records across incidents, requests, tasks, CI, assets, groups and users", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const requests: string[] = [];
  vi.stubGlobal("fetch", vi.fn(async (path: string) => {
    requests.push(path);
    const payload = path === "/api/v1/setup"
      ? { required: false }
      : path === "/api/v1/auth/me"
        ? {
            user: { id: "7", email: "ala@example.test", display_name: "Ala Kowalska", version: "1", credential_version: "1" },
            tenant: { id: "2", name: "Firma", slug: "firma" },
            permissions: [
              "incident.read_own",
              "request.read_own",
              "request.fulfill",
              "catalog.read",
              "assets.read",
              "cmdb.read",
              "admin.users.manage",
              "admin.rbac.manage",
            ],
            password_change_required: false,
            expires_at: new Date(Date.now() + 600_000).toISOString(),
          }
        : path.startsWith("/api/v1/incidents/")
          ? { incidents: [{ id: "12", number: "INC0012", title: "VPN niedostępny", state: "OPEN", priority: 2 }], next_after_id: "" }
          : path.startsWith("/api/v1/cmdb/assets?")
            ? { items: [{ id: "31", version: "1", asset_tag: "vpn-gw-01", name: "Brama VPN", owner_id: "7", owner_name: "Ala Kowalska", serial_number: "GW-01", vendor: "Net", model: "Edge", location: "Warszawa", state: "IN_USE", purchase_date: "", purchase_cost: "", currency: "PLN", warranty_until: "", archived: false, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z" }] }
            : path.startsWith("/api/v1/cmdb/cis?")
              ? { items: [{ id: "72", version: "1", identifier: "vpn-gw-ci", name: "VPN gateway CI", class: "VM", status: "ACTIVE", owner_id: "7", owner_name: "Ala Kowalska", environment: "PROD", location: "Warszawa", attributes: {}, archived: false, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z" }] }
            : path.startsWith("/api/v1/requests/")
              ? { items: [{ id: "41", number: "REQ0041", state: "SUBMITTED", items: [{ snapshot: { title: "VPN access" } }] }] }
              : path.startsWith("/api/v1/fulfillment/")
                ? { items: [{ id: "64", number: "SCTASK0064", request_number: "REQ0041", title: "Provision VPN", state: "OPEN", group_name: "IT Operations", assigned_name: "Ala" }] }
                : path.startsWith("/api/v1/admin/rbac/groups?")
                  ? { groups: [{ id: "51", name: "IT Operations", description: "IT support", member_count: "4" }] }
                  : path.startsWith("/api/v1/admin/users/")
                    ? { users: [{ id: "8", email: "operator@example.test", display_name: "Jan Operator", user_active: true, membership_active: true, created_at: "2026-01-01T00:00:00Z" }] }
                    : { items: [{ id: "71", title: "VPN access", description: "Access request" }] };
    return new Response(JSON.stringify(payload), { headers: { "Content-Type": "application/json" } });
  }));

  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () => root.render(
      <MemoryRouter initialEntries={["/search?q=VPN"]}>
        <AuthProvider>
          <Routes>
            <Route path="/search" element={<GlobalSearchPage />} />
          </Routes>
        </AuthProvider>
      </MemoryRouter>,
    ));
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });

    expect(container.textContent).toContain("VPN niedostępny");
    expect(container.textContent).toContain("vpn-gw-01");
    expect(container.textContent).toContain("vpn-gw-ci");
    expect(container.textContent).toContain("Provision VPN");
    expect(container.textContent).toContain("IT Operations");
    expect(container.textContent).toContain("Jan Operator");
    expect(container.querySelector('a[href="/incidents/12"]')).not.toBeNull();
    expect(container.querySelector('a[href="/assets/31"]')).not.toBeNull();
    expect(container.querySelector('a[href="/cmdb/72?return=%2Fcmdb%3Fq%3DVPN"]')).not.toBeNull();
    expect(container.querySelector('a[href="/fulfillment/64"]')).not.toBeNull();
    expect(container.querySelector('a[href="/rbac?kind=groups&id=51"]')).not.toBeNull();
    expect(container.querySelector('a[href="/users/8"]')).not.toBeNull();
    expect(requests).toContain("/api/v1/incidents/?q=VPN&limit=10");
    expect(requests).toContain("/api/v1/cmdb/cis?q=VPN&limit=10");
    expect(requests).toContain("/api/v1/fulfillment/?q=VPN&limit=10&view=group");
    expect(requests.some((path) => path.startsWith("/api/v1/admin/rbac/groups?"))).toBe(true);
    expect(requests.some((path) => path.startsWith("/api/v1/admin/users/?"))).toBe(true);
  } finally {
    await act(async () => root.unmount());
  }
});
