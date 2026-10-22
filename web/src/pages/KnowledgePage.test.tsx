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
    else if (path.startsWith("/api/v1/knowledge/spaces/10/pages?")) body = { pages: [{ id: "20", space_id: "10", title: "Start", status: "PUBLISHED", version: "1", updated_by: "1", updated_by_name: "Admin", updated_at: now }] };
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
  } finally { await act(async () => root.unmount()); }
});
