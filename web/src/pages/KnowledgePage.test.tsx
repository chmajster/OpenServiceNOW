import { act } from "react";
import { createRoot } from "react-dom/client";
import { createMemoryRouter, Link, RouterProvider } from "react-router-dom";
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
    else if (path === "/api/v1/knowledge/spaces?limit=100") body = { spaces: [spaces[0]], next_after_id: "10" };
    else if (path === "/api/v1/knowledge/spaces?limit=100&after_id=10") body = { spaces: [spaces[1]] };
    else if (path.startsWith("/api/v1/knowledge/search?")) body = { pages: [{ id: "31", space_id: "11", space_name: "Aplikacje", title: "Odzyskiwanie dostępu", status: "PUBLISHED", version: "1", updated_by: "1", updated_by_name: "Admin", updated_at: now, excerpt: "Instrukcja logowania" }] };
    else throw new Error(`unexpected request: ${path}`);
    return new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
  }));
  const router = createMemoryRouter([{ path: "/knowledge", element: <AuthProvider><KnowledgePage /></AuthProvider> }], { initialEntries: ["/knowledge"] });
  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () => { root.render(<RouterProvider router={router} />); await new Promise((resolve) => setTimeout(resolve, 20)); });
    const loadBases = container.querySelector<HTMLButtonElement>(".knowledge-pagination button");
    expect(loadBases?.textContent).toContain("Wczytaj kolejne 100 baz");
    await act(async () => { loadBases?.click(); await new Promise((resolve) => setTimeout(resolve, 20)); });
    expect(container.textContent).toContain("Aplikacje");
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

it("loads older article versions when the history is paginated", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const now = new Date().toISOString();
  const space = { id: "10", key: "OPS", name: "Operacje", description: "", status: "ACTIVE", review_interval_days: 30, version: "1", page_count: "1", subscribed: false, created_at: now, updated_at: now };
  const versions = Array.from({ length: 20 }, (_, index) => ({ page_id: "20", version: String(21 - index), title: `Zmiana ${21 - index}`, status: "PUBLISHED", changed_by: "1", changed_by_name: "Admin", changed_at: now }));
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input);
    let body: unknown;
    if (path === "/api/v1/setup") body = { required: false };
    else if (path === "/api/v1/auth/me") body = {
      user: { id: "1", email: "admin@example.test", display_name: "Admin", version: "1", credential_version: "1" },
      tenant: { id: "2", name: "Test", slug: "test" },
      permissions: ["knowledge.manage"], password_change_required: false, expires_at: new Date(Date.now() + 600_000).toISOString(),
    };
    else if (path === "/api/v1/knowledge/spaces?limit=100") body = { spaces: [space] };
    else if (path === "/api/v1/knowledge/spaces/10") body = space;
    else if (path === "/api/v1/knowledge/spaces/10/pages?limit=100") body = { pages: [] };
    else if (path === "/api/v1/knowledge/pages/20") body = { id: "20", space_id: "10", title: "Procedura", body: "Treść", status: "PUBLISHED", version: "21", updated_by: "1", updated_by_name: "Admin", created_by: "1", created_at: now, updated_at: now };
    else if (path === "/api/v1/knowledge/pages/20/versions?limit=20") body = { versions, next_before_version: "2" };
    else if (path === "/api/v1/knowledge/pages/20/versions?limit=20&before_version=2") body = { versions: [{ page_id: "20", version: "1", title: "Pierwsza wersja", status: "DRAFT", changed_by: "1", changed_by_name: "Admin", changed_at: now }] };
    else throw new Error(`unexpected request: ${path}`);
    return new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
  }));
  const router = createMemoryRouter([{ path: "/knowledge/:spaceID/articles/:pageID", element: <AuthProvider><KnowledgePage /></AuthProvider> }], { initialEntries: ["/knowledge/10/articles/20"] });
  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () => { root.render(<RouterProvider router={router} />); await new Promise((resolve) => setTimeout(resolve, 20)); });
    expect(container.textContent).not.toContain("Pierwsza wersja");
    const more = Array.from(container.querySelectorAll("button")).find((button) => button.textContent === "Wczytaj starsze wersje");
    expect(more).toBeTruthy();
    const labels = () => Array.from(container.querySelectorAll(".knowledge-history li strong")).map((item) => item.textContent);
    expect(labels()).toHaveLength(20);
    expect(labels()).not.toContain("Wersja 1");
    await act(async () => { more?.click(); await new Promise((resolve) => setTimeout(resolve, 20)); });
    expect(labels()).toContain("Wersja 1");
    expect(Array.from(container.querySelectorAll("button")).some((button) => button.textContent === "Wczytaj starsze wersje")).toBe(false);
  } finally { await act(async () => root.unmount()); }
});

it("refreshes a stale base version after a conflict and preserves the unsaved form", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const now = new Date().toISOString();
  const space = { id: "10", key: "OPS", name: "Operacje", description: "Opis", status: "ACTIVE", review_interval_days: 90, version: "1", page_count: "0", subscribed: false, created_at: now, updated_at: now };
  const latest = { ...space, name: "Operacje zdalnie", version: "2" };
  let spaceReads = 0;
  let updateCount = 0;
  const updates: Array<Record<string, unknown>> = [];
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input);
    let body: unknown;
    if (path === "/api/v1/setup") body = { required: false };
    else if (path === "/api/v1/auth/me") body = {
      user: { id: "1", email: "admin@example.test", display_name: "Admin", version: "1", credential_version: "1" },
      tenant: { id: "2", name: "Test", slug: "test" },
      permissions: ["knowledge.manage"], password_change_required: false, expires_at: new Date(Date.now() + 600_000).toISOString(),
    };
    else if (path === "/api/v1/knowledge/spaces?limit=100") body = { spaces: [space] };
    else if (path === "/api/v1/knowledge/spaces/10" && init?.method === "PUT") {
      updateCount += 1;
      updates.push(JSON.parse(String(init.body)) as Record<string, unknown>);
      if (updateCount === 1) return new Response(JSON.stringify({ error: { code: "KNOWLEDGE_CONFLICT", message: "Version conflict" } }), { status: 409, headers: { "Content-Type": "application/json" } });
      body = { ...latest, name: updates[1]!.name, version: "3" };
    }
    else if (path === "/api/v1/knowledge/spaces/10") { spaceReads += 1; body = spaceReads === 1 ? space : latest; }
    else if (path === "/api/v1/knowledge/spaces/10/pages?limit=100") body = { pages: [] };
    else throw new Error(`unexpected request: ${path} ${init?.method ?? "GET"}`);
    return new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
  }));
  const router = createMemoryRouter([{ path: "/knowledge/:spaceID", element: <AuthProvider><KnowledgePage /></AuthProvider> }], { initialEntries: ["/knowledge/10?view=settings"] });
  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () => { root.render(<RouterProvider router={router} />); await new Promise((resolve) => setTimeout(resolve, 20)); });
    const nameInput = Array.from(container.querySelectorAll("label")).find((label) => label.textContent?.startsWith("Nazwa"))?.querySelector("input");
    expect(nameInput).toBeTruthy();
    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
      setter?.call(nameInput, "Moja nazwa");
      nameInput?.dispatchEvent(new Event("input", { bubbles: true }));
    });
    const form = container.querySelector<HTMLFormElement>("form.panel.form-stack")!;
    await act(async () => { form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })); await new Promise((resolve) => setTimeout(resolve, 20)); });
    expect(updates).toHaveLength(1);
    const firstUpdate = updates[0]!;
    expect(firstUpdate.version).toBe("1");
    expect(container.textContent).toContain("Operacje zdalnie");
    expect(nameInput?.value).toBe("Moja nazwa");
    const keepDraft = Array.from(container.querySelectorAll("button")).find((button) => button.textContent?.includes("Zachowaj mój formularz"));
    expect(keepDraft).toBeTruthy();
    await act(async () => keepDraft?.click());
    await act(async () => { form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })); await new Promise((resolve) => setTimeout(resolve, 20)); });
    expect(updates).toHaveLength(2);
    const secondUpdate = updates[1]!;
    expect(secondUpdate.version).toBe("2");
    expect(secondUpdate.name).toBe("Moja nazwa");
    expect(container.textContent).toContain("Konfiguracja bazy wiedzy została zapisana.");
  } finally { await act(async () => root.unmount()); }
});

it("keeps article conflict resolution visible after edits and rebases the preserved draft", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const now = new Date().toISOString();
  const space = { id: "10", key: "OPS", name: "Operacje", description: "", status: "ACTIVE", review_interval_days: 30, version: "1", page_count: "1", subscribed: false, created_at: now, updated_at: now };
  const summary = { id: "20", space_id: "10", title: "Tytuł początkowy", status: "PUBLISHED", version: "1", updated_by: "1", updated_by_name: "Admin", updated_at: now };
  let current = { id: "20", space_id: "10", title: "Tytuł początkowy", body: "Treść początkowa", status: "PUBLISHED", version: "1", created_by: "1", updated_by: "1", updated_by_name: "Admin", created_at: now, updated_at: now };
  let updateCount = 0;
  const updates: Array<Record<string, unknown>> = [];
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input);
    let body: unknown;
    if (path === "/api/v1/setup") body = { required: false };
    else if (path === "/api/v1/auth/me") body = {
      user: { id: "1", email: "admin@example.test", display_name: "Admin", version: "1", credential_version: "1" },
      tenant: { id: "2", name: "Test", slug: "test" },
      permissions: ["knowledge.manage"], password_change_required: false, expires_at: new Date(Date.now() + 600_000).toISOString(),
    };
    else if (path === "/api/v1/knowledge/spaces?limit=100") body = { spaces: [space] };
    else if (path === "/api/v1/knowledge/spaces/10") body = space;
    else if (path === "/api/v1/knowledge/spaces/10/pages?limit=100") body = { pages: [summary] };
    else if (path === "/api/v1/knowledge/pages/20/versions?limit=20") body = { versions: [] };
    else if (path === "/api/v1/knowledge/pages/20" && init?.method === "PUT") {
      updateCount += 1;
      const input = JSON.parse(String(init.body)) as Record<string, unknown>;
      updates.push(input);
      if (updateCount === 1) {
        current = { ...current, title: "Tytuł zdalny", body: "Nowsza treść", version: "2" };
        return new Response(JSON.stringify({ error: { code: "KNOWLEDGE_CONFLICT", message: "Version conflict" } }), { status: 409, headers: { "Content-Type": "application/json" } });
      }
      current = { ...current, title: String(input.title), body: String(input.body), version: "3" };
      body = current;
    }
    else if (path === "/api/v1/knowledge/pages/20") body = current;
    else throw new Error(`unexpected request: ${path} ${init?.method ?? "GET"}`);
    return new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
  }));
  const router = createMemoryRouter([{ path: "/knowledge/:spaceID/articles/:pageID", element: <AuthProvider><KnowledgePage /></AuthProvider> }], { initialEntries: ["/knowledge/10/articles/20"] });
  const container = document.createElement("div");
  const root = createRoot(container);
  const editField = (value: string) => {
    const input = Array.from(container.querySelectorAll("label")).find((label) => label.textContent?.startsWith("Krótki opis"))?.querySelector("input");
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
    setter?.call(input, value);
    input?.dispatchEvent(new Event("input", { bubbles: true }));
  };
  try {
    await act(async () => { root.render(<RouterProvider router={router} />); await new Promise((resolve) => setTimeout(resolve, 20)); });
    const edit = Array.from(container.querySelectorAll("button")).find((button) => button.textContent?.includes("Edytuj artykuł"));
    await act(async () => edit?.click());
    await act(async () => editField("Moja zmiana"));
    const form = container.querySelector<HTMLFormElement>("form.knowledge-editor")!;
    await act(async () => { form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })); await new Promise((resolve) => setTimeout(resolve, 20)); });
    expect(updates[0]?.version).toBe("1");
    expect(container.textContent).toContain("Tytuł zdalny");
    await act(async () => editField("Moja zmiana zachowana"));
    expect(container.textContent).toContain("Artykuł ma nowszą wersję.");
    const save = Array.from(form.querySelectorAll<HTMLButtonElement>("button")).find((button) => button.textContent?.includes("Zapisz artykuł"));
    expect(save?.disabled).toBe(true);
    const keep = Array.from(container.querySelectorAll("button")).find((button) => button.textContent?.includes("Zachowaj moje zmiany"));
    await act(async () => { keep?.click(); await new Promise((resolve) => setTimeout(resolve, 20)); });
    expect(container.textContent).not.toContain("Artykuł ma nowszą wersję.");
    expect(container.querySelector<HTMLInputElement>("input")?.value).not.toBe("Tytuł zdalny");
    expect(save?.disabled).toBe(false);
    await act(async () => { form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })); await new Promise((resolve) => setTimeout(resolve, 20)); });
    expect(updates).toHaveLength(2);
    expect(updates[1]?.version).toBe("2");
    expect(updates[1]?.title).toBe("Moja zmiana zachowana");
    expect(container.textContent).toContain("Moja zmiana zachowana");
  } finally { await act(async () => root.unmount()); }
});

it("requests article-state filtering from the server before pagination", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const now = new Date().toISOString();
  const space = { id: "10", key: "OPS", name: "Operacje", description: "", status: "ACTIVE", review_interval_days: 30, version: "1", page_count: "2", subscribed: false, created_at: now, updated_at: now };
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
    else if (path === "/api/v1/knowledge/spaces?limit=100") body = { spaces: [space] };
    else if (path === "/api/v1/knowledge/spaces/10") body = space;
    else if (path === "/api/v1/knowledge/spaces/10/pages?limit=100&status=DRAFT") body = { pages: [{ id: "21", space_id: "10", title: "Późny szkic", status: "DRAFT", version: "1", updated_by: "1", updated_by_name: "Admin", updated_at: now }] };
    else throw new Error(`unexpected request: ${path}`);
    return new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
  }));
  const router = createMemoryRouter([{ path: "/knowledge/:spaceID", element: <AuthProvider><KnowledgePage /></AuthProvider> }], { initialEntries: ["/knowledge/10?view=draft"] });
  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () => { root.render(<RouterProvider router={router} />); await new Promise((resolve) => setTimeout(resolve, 20)); });
    expect(requests).toContain("/api/v1/knowledge/spaces/10/pages?limit=100&status=DRAFT");
    expect(container.textContent).toContain("Późny szkic");
  } finally { await act(async () => root.unmount()); }
});

it("refreshes subscription cards and discards subscription overrides when the session changes", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const now = new Date().toISOString();
  let userID = "1";
  const subscriptions = new Map([["1", false], ["2", true]]);
  let spaceListReads = 0;
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input);
    let body: unknown;
    if (path === "/api/v1/setup") body = { required: false };
    else if (path === "/api/v1/auth/me") body = {
      user: { id: userID, email: `${userID}@example.test`, display_name: `User ${userID}`, version: "1", credential_version: "1" },
      tenant: { id: "2", name: "Test", slug: "test" },
      permissions: ["knowledge.manage"], password_change_required: false, expires_at: new Date(Date.now() + 600_000).toISOString(),
    };
    else if (path === "/api/v1/knowledge/spaces?limit=100") {
      spaceListReads += 1;
      body = { spaces: [{ id: "10", key: "OPS", name: "Operacje", description: "", status: "ACTIVE", review_interval_days: 90, version: "1", page_count: "0", subscribed: subscriptions.get(userID), created_at: now, updated_at: now }] };
    }
    else if (path === "/api/v1/knowledge/spaces/10") body = { id: "10", key: "OPS", name: "Operacje", description: "", status: "ACTIVE", review_interval_days: 90, version: "1", page_count: "0", subscribed: subscriptions.get(userID), created_at: now, updated_at: now };
    else if (path === "/api/v1/knowledge/spaces/10/pages?limit=100") body = { pages: [] };
    else if (path === "/api/v1/knowledge/spaces/10/subscription") {
      subscriptions.set(userID, init?.method === "PUT");
      body = { id: "10", key: "OPS", name: "Operacje", description: "", status: "ACTIVE", review_interval_days: 90, version: "1", page_count: "0", subscribed: subscriptions.get(userID), created_at: now, updated_at: now };
    }
    else throw new Error(`unexpected request: ${path} ${init?.method ?? "GET"}`);
    return new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
  }));
  const router = createMemoryRouter([{ path: "/knowledge/:spaceID?", element: <AuthProvider><KnowledgePage /></AuthProvider> }], { initialEntries: ["/knowledge/10"] });
  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () => { root.render(<RouterProvider router={router} />); await new Promise((resolve) => setTimeout(resolve, 20)); });
    const subscriptionButton = () => Array.from(container.querySelectorAll<HTMLButtonElement>("button")).find((button) => button.textContent === "Subskrybuj" || button.textContent === "Anuluj subskrypcję");
    await act(async () => { subscriptionButton()?.click(); await new Promise((resolve) => setTimeout(resolve, 20)); });
    expect(subscriptionButton()?.textContent).toBe("Anuluj subskrypcję");
    expect(spaceListReads).toBeGreaterThan(1);

    const allBases = Array.from(container.querySelectorAll<HTMLAnchorElement>("a")).find((link) => link.textContent?.includes("Wszystkie bazy"));
    await act(async () => { allBases?.click(); await new Promise((resolve) => setTimeout(resolve, 20)); });
    expect(container.textContent).toContain("Subskrybowana");
    const baseCard = container.querySelector<HTMLAnchorElement>(".knowledge-base-card");
    await act(async () => { baseCard?.click(); await new Promise((resolve) => setTimeout(resolve, 20)); });

    await act(async () => { subscriptionButton()?.click(); await new Promise((resolve) => setTimeout(resolve, 20)); });
    expect(subscriptionButton()?.textContent).toBe("Subskrybuj");
    userID = "2";
    await act(async () => { window.dispatchEvent(new Event("focus")); await new Promise((resolve) => setTimeout(resolve, 30)); });
    expect(subscriptionButton()?.textContent).toBe("Anuluj subskrypcję");
  } finally { await act(async () => root.unmount()); }
});

it("warns before leaving dirty knowledge base settings", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const now = new Date().toISOString();
  const space = { id: "10", key: "OPS", name: "Operacje", description: "Opis", status: "ACTIVE", review_interval_days: 90, version: "1", page_count: "0", subscribed: false, created_at: now, updated_at: now };
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input);
    let body: unknown;
    if (path === "/api/v1/setup") body = { required: false };
    else if (path === "/api/v1/auth/me") body = {
      user: { id: "1", email: "admin@example.test", display_name: "Admin", version: "1", credential_version: "1" },
      tenant: { id: "2", name: "Test", slug: "test" },
      permissions: ["knowledge.manage"], password_change_required: false, expires_at: new Date(Date.now() + 600_000).toISOString(),
    };
    else if (path === "/api/v1/knowledge/spaces?limit=100") body = { spaces: [space] };
    else if (path === "/api/v1/knowledge/spaces/10") body = space;
    else throw new Error(`unexpected request: ${path}`);
    return new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
  }));
  const router = createMemoryRouter([{ path: "/knowledge/:spaceID", element: <AuthProvider><KnowledgePage /></AuthProvider> }], { initialEntries: ["/knowledge/10?view=settings"] });
  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () => { root.render(<RouterProvider router={router} />); await new Promise((resolve) => setTimeout(resolve, 20)); });
    const nameInput = Array.from(container.querySelectorAll("label")).find((label) => label.textContent?.startsWith("Nazwa"))?.querySelector<HTMLInputElement>("input");
    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
      setter?.call(nameInput, "Nowa nazwa");
      nameInput?.dispatchEvent(new Event("input", { bubbles: true }));
    });
    const allArticles = Array.from(container.querySelectorAll<HTMLButtonElement>("button")).find((button) => button.textContent === "Wszystkie");
    await act(async () => { allArticles?.click(); });
    expect(container.textContent).toContain("Niezapisana praca");
    expect(router.state.location.search).toBe("?view=settings");
    const stay = Array.from(container.querySelectorAll<HTMLButtonElement>("button")).find((button) => button.textContent === "Pozostań na stronie");
    await act(async () => { stay?.click(); });
    expect(nameInput?.value).toBe("Nowa nazwa");
    await act(async () => { allArticles?.click(); });
    const leave = Array.from(container.querySelectorAll<HTMLButtonElement>("button")).find((button) => button.textContent === "Opuść bez zapisu");
    await act(async () => { leave?.click(); });
    expect(router.state.location.search).toBe("");
    expect(container.textContent).not.toContain("Konfiguracja bazy wiedzy");
  } finally { await act(async () => root.unmount()); }
});

it("retries a knowledge search after its initial request fails", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const now = new Date().toISOString();
  const space = { id: "10", key: "OPS", name: "Operacje", description: "", status: "ACTIVE", review_interval_days: 90, version: "1", page_count: "1", subscribed: false, created_at: now, updated_at: now };
  let searchReads = 0;
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input);
    let body: unknown;
    if (path === "/api/v1/setup") body = { required: false };
    else if (path === "/api/v1/auth/me") body = {
      user: { id: "1", email: "admin@example.test", display_name: "Admin", version: "1", credential_version: "1" },
      tenant: { id: "2", name: "Test", slug: "test" },
      permissions: ["knowledge.manage"], password_change_required: false, expires_at: new Date(Date.now() + 600_000).toISOString(),
    };
    else if (path === "/api/v1/knowledge/spaces?limit=100") body = { spaces: [space] };
    else if (path === "/api/v1/knowledge/spaces/10") body = space;
    else if (path === "/api/v1/knowledge/spaces/10/search?limit=100&q=procedura") {
      searchReads += 1;
      if (searchReads === 1) return new Response(JSON.stringify({ error: { code: "TEMPORARY_FAILURE", message: "Usługa wyszukiwania jest chwilowo niedostępna." } }), { status: 503, headers: { "Content-Type": "application/json" } });
      body = { pages: [{ id: "20", space_id: "10", space_name: "Operacje", title: "Procedura odzyskiwania", status: "PUBLISHED", version: "1", updated_by: "1", updated_by_name: "Admin", updated_at: now, excerpt: "Kroki odzyskiwania dostępu" }] };
    }
    else throw new Error(`unexpected request: ${path}`);
    return new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
  }));
  const router = createMemoryRouter([{ path: "/knowledge/:spaceID", element: <AuthProvider><KnowledgePage /></AuthProvider> }], { initialEntries: ["/knowledge/10?view=search&q=procedura"] });
  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () => { root.render(<RouterProvider router={router} />); await new Promise((resolve) => setTimeout(resolve, 20)); });
    expect(container.textContent).toContain("Wyszukiwanie nie powiodło się");
    const retry = Array.from(container.querySelectorAll<HTMLButtonElement>("button")).find((button) => button.textContent === "Spróbuj ponownie");
    expect(retry).toBeTruthy();
    await act(async () => { retry?.click(); await new Promise((resolve) => setTimeout(resolve, 20)); });
    expect(searchReads).toBe(2);
    expect(container.textContent).toContain("Procedura odzyskiwania");
  } finally { await act(async () => root.unmount()); }
});

it("protects a new-base draft from navigation while allowing explicit cancellation", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input);
    let body: unknown;
    if (path === "/api/v1/setup") body = { required: false };
    else if (path === "/api/v1/auth/me") body = {
      user: { id: "1", email: "admin@example.test", display_name: "Admin", version: "1", credential_version: "1" },
      tenant: { id: "2", name: "Test", slug: "test" },
      permissions: ["knowledge.manage"], password_change_required: false, expires_at: new Date(Date.now() + 600_000).toISOString(),
    };
    else if (path === "/api/v1/knowledge/spaces?limit=100") body = { spaces: [] };
    else throw new Error(`unexpected request: ${path}`);
    return new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
  }));
  function KnowledgeRoute() { return <><Link to="/other">Inny moduł</Link><AuthProvider><KnowledgePage /></AuthProvider></>; }
  const router = createMemoryRouter([
    { path: "/knowledge", element: <KnowledgeRoute /> },
    { path: "/other", element: <p>Inny ekran</p> },
  ], { initialEntries: ["/knowledge"] });
  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () => { root.render(<RouterProvider router={router} />); await new Promise((resolve) => setTimeout(resolve, 20)); });
    expect(container.querySelector(".knowledge-base-form")).toBeTruthy();
    const nameInput = Array.from(container.querySelectorAll("label")).find((label) => label.textContent?.startsWith("Nazwa"))?.querySelector<HTMLInputElement>("input");
    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
      setter?.call(nameInput, "Szkic bazy");
      nameInput?.dispatchEvent(new Event("input", { bubbles: true }));
    });
    const otherModule = Array.from(container.querySelectorAll<HTMLAnchorElement>("a")).find((link) => link.textContent === "Inny moduł");
    await act(async () => { otherModule?.click(); });
    expect(container.textContent).toContain("Niezapisana praca");
    expect(router.state.location.pathname).toBe("/knowledge");
    expect(nameInput?.value).toBe("Szkic bazy");
    const stay = Array.from(container.querySelectorAll<HTMLButtonElement>("button")).find((button) => button.textContent === "Pozostań na stronie");
    await act(async () => { stay?.click(); });
    const cancel = Array.from(container.querySelectorAll<HTMLButtonElement>("button")).find((button) => button.textContent === "Anuluj");
    await act(async () => { cancel?.click(); });
    expect(container.querySelector(".knowledge-base-form")).toBeNull();
    expect(container.textContent).toContain("Brak baz wiedzy");
  } finally { await act(async () => root.unmount()); }
});

it("clears an open base-creation form when the authenticated session changes", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  let userID = "1";
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input);
    let body: unknown;
    if (path === "/api/v1/setup") body = { required: false };
    else if (path === "/api/v1/auth/me") body = {
      user: { id: userID, email: `${userID}@example.test`, display_name: `User ${userID}`, version: "1", credential_version: "1" },
      tenant: { id: "2", name: "Test", slug: "test" },
      permissions: userID === "1" ? ["knowledge.manage"] : ["knowledge.read"], password_change_required: false, expires_at: new Date(Date.now() + 600_000).toISOString(),
    };
    else if (path === "/api/v1/knowledge/spaces?limit=100") body = { spaces: [] };
    else throw new Error(`unexpected request: ${path}`);
    return new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
  }));
  const router = createMemoryRouter([{ path: "/knowledge", element: <AuthProvider><KnowledgePage /></AuthProvider> }], { initialEntries: ["/knowledge"] });
  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () => { root.render(<RouterProvider router={router} />); await new Promise((resolve) => setTimeout(resolve, 20)); });
    const nameInput = Array.from(container.querySelectorAll("label")).find((label) => label.textContent?.startsWith("Nazwa"))?.querySelector<HTMLInputElement>("input");
    expect(nameInput).toBeTruthy();
    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
      setter?.call(nameInput, "Poufny szkic poprzedniego użytkownika");
      nameInput?.dispatchEvent(new Event("input", { bubbles: true }));
    });
    userID = "2";
    await act(async () => { window.dispatchEvent(new Event("focus")); await new Promise((resolve) => setTimeout(resolve, 40)); });
    expect(container.querySelector(".knowledge-base-form")).toBeNull();
    expect(container.textContent).not.toContain("Poufny szkic poprzedniego użytkownika");
    expect(container.textContent).toContain("Brak baz wiedzy");
  } finally { await act(async () => root.unmount()); }
});

it("updates the base view after resolving a configuration conflict with the latest record", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const now = new Date().toISOString();
  const space = { id: "10", key: "OPS", name: "Operacje", description: "Opis", status: "ACTIVE", review_interval_days: 90, version: "1", page_count: "0", subscribed: false, created_at: now, updated_at: now };
  const latest = { ...space, name: "Operacje archiwalne", description: "Zamknięty obszar", status: "ARCHIVED", version: "2" };
  let spaceReads = 0;
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input);
    let body: unknown;
    if (path === "/api/v1/setup") body = { required: false };
    else if (path === "/api/v1/auth/me") body = {
      user: { id: "1", email: "admin@example.test", display_name: "Admin", version: "1", credential_version: "1" },
      tenant: { id: "2", name: "Test", slug: "test" },
      permissions: ["knowledge.manage"], password_change_required: false, expires_at: new Date(Date.now() + 600_000).toISOString(),
    };
    else if (path === "/api/v1/knowledge/spaces?limit=100") body = { spaces: [space] };
    else if (path === "/api/v1/knowledge/spaces/10" && init?.method === "PUT") return new Response(JSON.stringify({ error: { code: "KNOWLEDGE_CONFLICT", message: "Version conflict" } }), { status: 409, headers: { "Content-Type": "application/json" } });
    else if (path === "/api/v1/knowledge/spaces/10") { spaceReads += 1; body = spaceReads === 1 ? space : latest; }
    else if (path === "/api/v1/knowledge/spaces/10/pages?limit=100") body = { pages: [] };
    else throw new Error(`unexpected request: ${path} ${init?.method ?? "GET"}`);
    return new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
  }));
  const router = createMemoryRouter([{ path: "/knowledge/:spaceID", element: <AuthProvider><KnowledgePage /></AuthProvider> }], { initialEntries: ["/knowledge/10?view=settings"] });
  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () => { root.render(<RouterProvider router={router} />); await new Promise((resolve) => setTimeout(resolve, 20)); });
    const form = container.querySelector<HTMLFormElement>("form.panel.form-stack")!;
    await act(async () => { form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })); await new Promise((resolve) => setTimeout(resolve, 20)); });
    expect(container.textContent).toContain("Baza została zmieniona przez inną osobę.");
    const loadLatest = Array.from(container.querySelectorAll<HTMLButtonElement>("button")).find((button) => button.textContent === "Wczytaj aktualną konfigurację");
    await act(async () => { loadLatest?.click(); await new Promise((resolve) => setTimeout(resolve, 20)); });
    expect(container.querySelector(".knowledge-center-header h1")?.textContent).toBe("Operacje archiwalne");
    expect(container.textContent).toContain("Ta baza wiedzy jest zarchiwizowana");
    const allArticles = Array.from(container.querySelectorAll<HTMLButtonElement>("button")).find((button) => button.textContent === "Wszystkie");
    await act(async () => { allArticles?.click(); await new Promise((resolve) => setTimeout(resolve, 20)); });
    expect(container.querySelector(".knowledge-center-header h1")?.textContent).toBe("Operacje archiwalne");
    expect(container.textContent).toContain("Ta baza wiedzy jest zarchiwizowana");
  } finally { await act(async () => root.unmount()); }
});
