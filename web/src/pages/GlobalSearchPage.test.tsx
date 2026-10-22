import { act } from "react";
import { createRoot } from "react-dom/client";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { AuthProvider } from "../auth/AuthContext";
import { GlobalSearchPage } from "./GlobalSearchPage";

afterEach(() => vi.unstubAllGlobals());

it("searches accessible incidents and assets and links to their record pages", async () => {
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
            permissions: ["incident.read_own", "assets.read"],
            password_change_required: false,
            expires_at: new Date(Date.now() + 600_000).toISOString(),
          }
        : path.startsWith("/api/v1/incidents/")
          ? { incidents: [{ id: "12", number: "INC0012", title: "VPN niedostępny", state: "OPEN", priority: 2 }], next_after_id: "" }
          : { items: [{ id: "31", version: "1", asset_tag: "vpn-gw-01", name: "Brama VPN", owner_id: "7", owner_name: "Ala Kowalska", serial_number: "GW-01", vendor: "Net", model: "Edge", location: "Warszawa", state: "IN_USE", purchase_date: "", purchase_cost: "", currency: "PLN", warranty_until: "", archived: false, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z" }] };
    return new Response(JSON.stringify(payload), { headers: { "Content-Type": "application/json" } });
  }));
  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () => root.render(
      <MemoryRouter initialEntries={["/search?q=VPN"]}>
        <AuthProvider><Routes><Route path="/search" element={<GlobalSearchPage />} /></Routes></AuthProvider>
      </MemoryRouter>,
    ));
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
    expect(container.textContent).toContain("VPN niedostępny");
    expect(container.textContent).toContain("vpn-gw-01");
    expect(container.querySelector('a[href="/incidents/12"]')).not.toBeNull();
    expect(container.querySelector('a[href="/assets/31"]')).not.toBeNull();
    expect(requests).toContain("/api/v1/incidents/?q=VPN&limit=10");
    expect(requests.some((path) => path.startsWith("/api/v1/cmdb/assets?"))).toBe(true);
    expect(requests.some((path) => path.includes("/api/v1/catalog/items"))).toBe(false);
  } finally {
    await act(async () => root.unmount());
  }
});
