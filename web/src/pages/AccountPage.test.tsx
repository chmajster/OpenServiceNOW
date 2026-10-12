import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";
import { AuthProvider, useAuth } from "../auth/AuthContext";
import { AccountPage } from "./AccountPage";

afterEach(() => vi.unstubAllGlobals());
it("resets the account editor on an identity change and reloads a changed session", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  let auth!: ReturnType<typeof useAuth>;
  let lists = 0;
  let current = {
    user: {
      id: "1",
      email: "a@example.test",
      display_name: "Account A",
      version: "1",
      credential_version: "1",
    },
    tenant: { id: "2", name: "Tenant", slug: "test" },
    permissions: [],
    password_change_required: false,
    expires_at: new Date(Date.now() + 600_000).toISOString(),
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      const body =
        path === "/api/v1/setup"
          ? { required: false }
          : path.startsWith("/api/v1/auth/sessions")
            ? (lists++, { sessions: [] })
            : current;
      return new Response(JSON.stringify(body), {
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
  function Probe() {
    auth = useAuth();
    return null;
  }
  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () =>
      root.render(
        <AuthProvider>
          <Probe />
          <AccountPage />
        </AuthProvider>,
      ),
    );
    expect(
      container.querySelector<HTMLInputElement>('input[autocomplete="name"]')!
        .value,
    ).toBe("Account A");
    current = {
      ...current,
      user: { ...current.user, id: "3", display_name: "Account B" },
    };
    await act(async () => auth.refresh());
    expect(
      container.querySelector<HTMLInputElement>('input[autocomplete="name"]')!
        .value,
    ).toBe("Account B");
    const previous = lists;
    current = {
      ...current,
      expires_at: new Date(Date.now() + 900_000).toISOString(),
    };
    await act(async () => auth.refresh());
    expect(lists).toBeGreaterThan(previous);
  } finally {
    await act(async () => root.unmount());
  }
});
