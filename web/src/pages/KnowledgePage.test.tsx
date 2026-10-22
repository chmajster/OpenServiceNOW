import { act } from "react";
import { createRoot } from "react-dom/client";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { AuthProvider } from "../auth/AuthContext";
import { KnowledgePage } from "./KnowledgePage";

afterEach(() => vi.unstubAllGlobals());

it("shows the ServiceNow-style Knowledge Center navigation", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const now = new Date().toISOString();
  const space = {
    id: "10", key: "OPS", name: "Operacje", description: "Wiedza zespołu",
    status: "ACTIVE", review_interval_days: 90, version: "1", page_count: "1",
    subscribed: false, created_at: now, updated_at: now,
  };
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input);
    let body: unknown;
    if (path === "/api/v1/setup") body = { required: false };
    else if (path === "/api/v1/auth/me") body = {
      user: { id: "1", email: "admin@example.test", display_name: "Admin", version: "1", credential_version: "1" },
      tenant: { id: "2", name: "Test", slug: "test" },
      permissions: ["knowledge.manage"], password_change_required: false, expires_at: new Date(Date.now() + 600_000).toISOString(),
    };
    else if (path.startsWith("/api/v1/knowledge/spaces?")) body = { spaces: [space] };
    else if (path === "/api/v1/knowledge/spaces/10") body = space;
    else if (path === "/api/v1/knowledge/spaces/10/pages?limit=100") body = { pages: [{ id: "20", space_id: "10", title: "Start", status: "PUBLISHED", version: "1", updated_by: "1", updated_by_name: "Admin", updated_at: now }], next_after_id: "20" };
    else if (path === "/api/v1/knowledge/spaces/10/pages?limit=100&after_id=20") body = { pages: [{ id: "21", space_id: "10", title: "Dalszy artykuł", status: "DRAFT", version: "1", updated_by: "1", updated_by_name: "Admin", updated_at: now }] };
    else throw new Error(`unexpected request: ${path}`);
    return new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
  }));
  const router = createMemoryRouter([{ path: "/knowledge/:spaceID", element: <AuthProvider><KnowledgePage /></AuthProvider> }], { initialEntries: ["/knowledge/10"] });
  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () => { root.render(<RouterProvider router={router} />); await new Promise((resolve) => setTimeout(resolve, 20)); });
    const text = container.textContent ?? "";
    for (const label of ["Knowledge Center", "Nowy artykuł", "Subskrybuj", "Wszystkie bazy", "Nieopublikowane", "Opublikowane", "Wycofane", "Wygasające", "Wyszukiwanie", "Konfiguracja bazy", "KB0000020"]) {
      expect(text).toContain(label);
    }
    for (const removed of ["Więcej działań", "Utwórz stronę", "Eksport ZIP", "Import", "Szablony"]) {
      expect(text).not.toContain(removed);
    }
    expect(text).toContain("Start");
    const more = container.querySelector<HTMLButtonElement>(".knowledge-pagination button");
    expect(more?.textContent).toContain("Wczytaj kolejne 100 artykułów");
    await act(async () => { more?.click(); await new Promise((resolve) => setTimeout(resolve, 20)); });
    expect(container.textContent).toContain("Dalszy artykuł");
    expect(container.querySelector(".knowledge-pagination")).toBeNull();
  } finally { await act(async () => root.unmount()); }
});

it("searches across knowledge bases from the portal and links to the matching base", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const now = new Date().toISOString();
  const spaces = [
    { id: "10", key: "OPS", name: "Operacje", description: "", status: "ACTIVE", review_interval_days: 90, version: "1", page_count: "1", subscribed: false, created_at: now, updated_at: now },
    { id: "11", key: "APPS", name: "Aplikacje", description: "", status: "ACTIVE", review_interval_days: 90, version: "1", page_count: "1", subscribed: false, created_at: now, updated_at: now },
  ];
  const requests: string[] = [];
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input);
    requests.push(path);
    let body: unknown;
    if (path === "/api/v1/setup") body = { required: false };
    else if (path === "/api/v1/auth/me") body = {
      user: { id: "1", email: "admin@example.test", display_name: "Admin", version: "1", credential_version: "1" },
      tenant: { id: "2", name: "Test", slug: "test" },
      permissions: ["knowledge.manage"], password_change_required: false, expires_at: new Date(Date.now() + 600_000).toISOString(),
    };
    else if (path.startsWith("/api/v1/knowledge/spaces?")) body = { spaces };
    else if (path.startsWith("/api/v1/knowledge/search?")) body = { pages: [{ id: "31", space_id: "11", space_name: "Aplikacje", title: "Odzyskiwanie dostępu", status: "PUBLISHED", version: "1", updated_by: "1", updated_by_name: "Admin", updated_at: now, excerpt: "Instrukcja logowania" }] };
    else throw new Error(`unexpected request: ${path}`);
    return new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
  }));
  const router = createMemoryRouter([{ path: "/knowledge", element: <AuthProvider><KnowledgePage /></AuthProvider> }], { initialEntries: ["/knowledge"] });
  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () => { root.render(<RouterProvider router={router} />); await new Promise((resolve) => setTimeout(resolve, 20)); });
    const input = container.querySelector<HTMLInputElement>(".knowledge-hero-search input")!;
    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
      setter?.call(input, "odzyskiwanie");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    const form = container.querySelector<HTMLFormElement>(".knowledge-hero-search")!;
    await act(async () => { form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })); await new Promise((resolve) => setTimeout(resolve, 20)); });
    expect(requests.some((path) => path === "/api/v1/knowledge/search?limit=100&q=odzyskiwanie")).toBe(true);
    expect(container.textContent).toContain("Aplikacje");
    expect(container.textContent).toContain("Odzyskiwanie dostępu");
    expect(container.querySelector<HTMLAnchorElement>(".knowledge-search-results a")?.getAttribute("href")).toBe("/knowledge/11/articles/31");
  } finally { await act(async () => root.unmount()); }
});
