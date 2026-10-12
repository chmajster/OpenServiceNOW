import { act, useState } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";
import { useRemotePage } from "./useRemotePage";
afterEach(() => vi.unstubAllGlobals());
it("hides A immediately and ignores its late response after switching to B", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const requests = new Map<string, (response: Response) => void>();
  vi.stubGlobal(
    "fetch",
    vi.fn(
      (path: string) =>
        new Promise<Response>((resolve) => {
          requests.set(path, resolve);
        }),
    ),
  ); // Deliberately ignores abort.
  let select!: (id: string) => void;
  function Screen() {
    const [id, setID] = useState("A");
    select = setID;
    const page = useRemotePage<{ name: string }>(
      `/users/${id}/roles?limit=50`,
      "roles",
    );
    return (
      <div>
        <button disabled={page.loading || !!page.error}>Usuń</button>
        <span>{page.items.map((role) => role.name).join(",")}</span>
      </div>
    );
  }
  const container = document.createElement("div");
  const root = createRoot(container);
  const respond = (path: string, name: string) =>
    requests.get(path)!(
      new Response(JSON.stringify({ roles: [{ name }] }), {
        headers: { "Content-Type": "application/json" },
      }),
    );
  try {
    await act(async () => root.render(<Screen />));
    await act(async () => select("B"));
    expect(container.querySelector("button")?.disabled).toBe(true);
    await act(async () => respond("/users/B/roles?limit=50", "B role"));
    expect(container.textContent).toContain("B role");
    await act(async () => respond("/users/A/roles?limit=50", "A role"));
    expect(container.textContent).not.toContain("A role");
    expect(container.textContent).toContain("B role");
  } finally {
    await act(async () => root.unmount());
  }
});
