import { act } from "react";
import { createRoot } from "react-dom/client";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { AuthProvider } from "../auth/AuthContext";
import { AssetsPage } from "./AssetsPage";

afterEach(() => vi.unstubAllGlobals());

it("shows tenant authorized assets and links each record to its workspace", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const asset = {
    id: "5",
    version: "2",
    asset_tag: "laptop-05",
    name: "Laptop zespołu wsparcia",
    owner_id: "7",
    owner_name: "Ala Kowalska",
    serial_number: "SN-05",
    vendor: "Lenovo",
    model: "T14",
    location: "Warszawa",
    state: "IN_USE",
    purchase_date: "2026-01-01",
    purchase_cost: "4800.00",
    currency: "PLN",
    warranty_until: "2028-01-01",
    archived: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-02T00:00:00Z",
  };
  vi.stubGlobal("fetch", vi.fn(async (path: string) => {
    const payload = path === "/api/v1/setup"
      ? { required: false }
      : path === "/api/v1/auth/me"
        ? {
            user: { id: "7", email: "ala@example.test", display_name: "Ala Kowalska", version: "1", credential_version: "1" },
            tenant: { id: "2", name: "Firma", slug: "firma" },
            permissions: ["assets.read", "assets.manage"],
            password_change_required: false,
            expires_at: new Date(Date.now() + 600_000).toISOString(),
          }
        : { items: [asset] };
    return new Response(JSON.stringify(payload), { headers: { "Content-Type": "application/json" } });
  }));
  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () => root.render(
      <MemoryRouter initialEntries={["/assets"]}>
        <AuthProvider><Routes><Route path="/assets" element={<AssetsPage />} /></Routes></AuthProvider>
      </MemoryRouter>,
    ));
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
    expect(container.textContent).toContain("laptop-05");
    expect(container.textContent).toContain("Ala Kowalska");
    expect(container.querySelector('a[href="/assets/5"]')?.textContent).toContain("laptop-05");
    expect(container.querySelector('a[href="/assets/new"]')?.textContent).toBe("Nowy zasób");
  } finally {
    await act(async () => root.unmount());
  }
});
