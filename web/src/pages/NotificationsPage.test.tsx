import { act } from "react";
import { createRoot } from "react-dom/client";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { NotificationsPage } from "./NotificationsPage";
import { AuthProvider } from "../auth/AuthContext";

afterEach(() => vi.unstubAllGlobals());
it("keeps exact BIGINT notification links and an explicit zero unread count after read", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const id = "9007199254750019",
    incident = "9007199254750027";
  let read = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const payload =
        path === "/api/v1/setup"
          ? { required: false }
          : path === "/api/v1/auth/me"
            ? {
                user: {
                  id: "1",
                  email: "user@example.test",
                  display_name: "User",
                  version: "1",
                  credential_version: "1",
                },
                tenant: { id: "2", name: "Org", slug: "org" },
                permissions: ["incident.read_own"],
                password_change_required: false,
                expires_at: new Date(Date.now() + 600000).toISOString(),
              }
            : path.includes("/read") && init?.method === "POST"
              ? ((read = true), null)
              : {
                  notifications: [
                    {
                      id,
                      incident_id: incident,
                      source_event_id: "9007199254750049",
                      kind: "reply",
                      summary: "Nowa odpowiedź",
                      occurred_at: new Date().toISOString(),
                      ...(read ? { read_at: new Date().toISOString() } : {}),
                    },
                  ],
                  unread: read ? "0" : "1",
                };
      return payload === null
        ? new Response(null, { status: 204 })
        : new Response(JSON.stringify(payload), {
            headers: { "Content-Type": "application/json" },
          });
    }),
  );
  const container = document.createElement("div"),
    root = createRoot(container);
  try {
    await act(async () =>
      root.render(
        <MemoryRouter>
          <AuthProvider>
            <NotificationsPage />
          </AuthProvider>
        </MemoryRouter>,
      ),
    );
    expect(
      container.querySelector<HTMLAnchorElement>(
        `a[href="/incidents/${incident}"]`,
      )?.textContent,
    ).toContain(incident);
    const button = [...container.querySelectorAll("button")].find(
      (b) => b.textContent === "Oznacz jako przeczytane",
    )!;
    await act(async () => button.click());
    expect(container.textContent).toContain("Nieprzeczytane: 0");
    expect(container.textContent).toContain("Przeczytane");
  } finally {
    await act(async () => root.unmount());
  }
});
