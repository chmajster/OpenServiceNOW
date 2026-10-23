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

it("links and unlinks an IT asset with a CMDB item from the asset workspace", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  let assetVersion = "2";
  let ciVersion = "4";
  let linked = false;
  const linkBodies: Array<{ version: string; asset_id: string; asset_version: string }> = [];
  const asset = {
    id: "5",
    version: assetVersion,
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
  const ci = {
    id: "12",
    version: ciVersion,
    identifier: "CI-LAPTOP-05",
    name: "Laptop wsparcia",
    class: "WORKSTATION",
    status: "ACTIVE",
    owner_id: "7",
    owner_name: "Ala Kowalska",
    environment: "PROD",
    location: "Warszawa",
    attributes: {},
    custom_fields: {},
    custom_fields_version: "1",
    archived: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-02T00:00:00Z",
  };
  const fetchMock = vi.fn(async (path: string, init?: RequestInit) => {
    const method = init?.method ?? "GET";
    if (path === "/api/v1/setup") return new Response(JSON.stringify({ required: false }), { headers: { "Content-Type": "application/json" } });
    if (path === "/api/v1/auth/me") return new Response(JSON.stringify({
      user: { id: "7", email: "ala@example.test", display_name: "Ala Kowalska", version: "1", credential_version: "1" },
      tenant: { id: "2", name: "Firma", slug: "firma" },
      permissions: ["assets.read", "assets.manage", "cmdb.read", "cmdb.manage"],
      password_change_required: false,
      expires_at: new Date(Date.now() + 600_000).toISOString(),
    }), { headers: { "Content-Type": "application/json" } });
    if (path === "/api/v1/cmdb/assets/5" && method === "GET") return new Response(JSON.stringify({ ...asset, version: assetVersion }), { headers: { "Content-Type": "application/json" } });
    if (path === "/api/v1/cmdb/assets/5/ci") return new Response(JSON.stringify({ ci: linked ? { ...ci, version: ciVersion } : undefined }), { headers: { "Content-Type": "application/json" } });
    if (path === "/api/v1/cmdb/assets/5/history?limit=25") return new Response(JSON.stringify({ entries: [] }), { headers: { "Content-Type": "application/json" } });
    if (path.startsWith("/api/v1/cmdb/cis?") && method === "GET") return new Response(JSON.stringify({ items: [{ ...ci, version: ciVersion }] }), { headers: { "Content-Type": "application/json" } });
    if (path === "/api/v1/cmdb/cis/12" && method === "GET") return new Response(JSON.stringify({ ...ci, version: ciVersion }), { headers: { "Content-Type": "application/json" } });
    if (path === "/api/v1/cmdb/cis/12/asset" && method === "PUT") {
      const body = JSON.parse(String(init?.body)) as { version: string; asset_id: string; asset_version: string };
      linkBodies.push(body);
      linked = body.asset_id !== "0";
      assetVersion = String(Number(assetVersion) + 1);
      ciVersion = String(Number(ciVersion) + 1);
      return new Response(JSON.stringify({ ...ci, version: ciVersion }), { headers: { "Content-Type": "application/json" } });
    }
    return new Response(JSON.stringify({ error: { message: `Unexpected request ${method} ${path}` } }), { status: 500, headers: { "Content-Type": "application/json" } });
  });
  vi.stubGlobal("fetch", fetchMock);

  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  try {
    await act(async () => root.render(
      <MemoryRouter initialEntries={["/assets/5"]}>
        <AuthProvider><Routes><Route path="/assets/:assetID" element={<AssetsPage />} /></Routes></AuthProvider>
      </MemoryRouter>,
    ));
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
    expect(container.textContent).toContain("Ten zasób nie jest powiązany z elementem konfiguracji.");

    const combobox = container.querySelector<HTMLInputElement>('input[role="combobox"]');
    expect(combobox).not.toBeNull();
    await act(async () => { combobox!.click(); });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 275)); });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
    const option = container.querySelector<HTMLElement>('[role="option"]');
    expect(option?.textContent).toContain("CI-LAPTOP-05 · Laptop wsparcia");
    await act(async () => { option!.click(); await new Promise((resolve) => setTimeout(resolve, 0)); });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
    const linkButton = [...container.querySelectorAll("button")].find((button) => button.textContent === "Powiąż z CMDB");
    expect(linkButton?.disabled).toBe(false);
    await act(async () => { linkButton!.click(); await new Promise((resolve) => setTimeout(resolve, 0)); });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
    expect(linkBodies[0]).toEqual({ version: "4", asset_id: "5", asset_version: "2" });
    expect(container.querySelector('a[href="/cmdb/12"]')?.textContent).toBe("Laptop wsparcia");

    const unlinkButton = [...container.querySelectorAll("button")].find((button) => button.textContent === "Odłącz");
    expect(unlinkButton?.disabled).toBe(false);
    await act(async () => { unlinkButton!.click(); await new Promise((resolve) => setTimeout(resolve, 0)); });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
    expect(linkBodies[1]).toEqual({ version: "5", asset_id: "0", asset_version: "3" });
    expect(container.textContent).toContain("Ten zasób nie jest powiązany z elementem konfiguracji.");
  } finally {
    await act(async () => root.unmount());
    container.remove();
  }
});
