import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";
import { AuthProvider, useAuth } from "./AuthContext";
afterEach(() => vi.unstubAllGlobals());
it("refresh supersedes older reads, logout stays cleared and login reads fresh context", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  let meCount = 0;
  let finish!: (value: Response) => void;
  let auth!: ReturnType<typeof useAuth>;
  const json = (value: unknown) =>
    new Response(JSON.stringify(value), {
      headers: { "Content-Type": "application/json" },
    });
  const session = (name: string) => ({
    user: {
      id: "9007199254740993",
      email: "admin@example.test",
      display_name: name,
    },
    tenant: { id: "7", name: `Organizacja ${name}`, slug: "test" },
    permissions: ["tenant.read"],
    expires_at: new Date(Date.now() + 600_000).toISOString(),
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      if (path === "/api/v1/setup") return json({ required: false });
      if (path === "/api/v1/auth/me") {
        meCount++;
        if (meCount === 2 || meCount === 4)
          return new Promise<Response>((resolve) => {
            finish = resolve;
          });
        return json(session(meCount === 1 ? "Old" : "New"));
      }
      return new Response(null, { status: 204 });
    }),
  );
  function Screen() {
    auth = useAuth();
    return (
      <span>
        {auth.status}:{auth.session?.user.display_name}:
        {auth.session?.tenant.name}
      </span>
    );
  }
  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () =>
      root.render(
        <AuthProvider>
          <Screen />
        </AuthProvider>,
      ),
    );
    expect(container.textContent).toContain("authenticated:Old");
    let pending!: Promise<void>;
    await act(async () => {
      pending = auth.refresh();
    });
    // A mutation refresh must read after the write, even if a focus refresh is pending.
    await act(async () => {
      void auth.refresh();
    });
    expect(meCount).toBe(3);
    expect(container.textContent).toContain("authenticated:New");
    await act(async () => {
      finish(json(session("Old")));
      await pending;
    });
    expect(container.textContent).toContain("authenticated:New");
    await act(async () => {
      pending = auth.refresh();
    });
    await act(async () => auth.logout());
    expect(container.textContent).toBe("anonymous::");
    await act(async () => {
      finish(json(session("Old")));
      await pending;
    });
    expect(container.textContent).toBe("anonymous::");
    await act(async () =>
      auth.login({ tenant: "test", email: "admin", password: "test-password" }),
    );
    expect(container.textContent).toContain(
      "authenticated:New:Organizacja New",
    );
  } finally {
    await act(async () => root.unmount());
  }
});
it("reports a confirmed password change when the following session read fails", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  let auth!: ReturnType<typeof useAuth>;
  let changed = false;
  const json = (value: unknown, status = 200) =>
    new Response(JSON.stringify(value), {
      status,
      headers: { "Content-Type": "application/json" },
    });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      if (path === "/api/v1/setup") return json({ required: false });
      if (path === "/api/v1/auth/password") {
        changed = true;
        return json({ password_change_required: false });
      }
      if (changed)
        return json(
          { error: { code: "ACCOUNT_FAILED", message: "Read failed" } },
          500,
        );
      return json({
        user: {
          id: "1",
          email: "user@example.test",
          display_name: "User",
          version: "1",
          credential_version: "1",
        },
        tenant: { id: "2", name: "Tenant", slug: "test" },
        permissions: [],
        password_change_required: false,
        expires_at: new Date(Date.now() + 600_000).toISOString(),
      });
    }),
  );
  function Screen() {
    auth = useAuth();
    return <span>{auth.error}</span>;
  }
  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () =>
      root.render(
        <AuthProvider>
          <Screen />
        </AuthProvider>,
      ),
    );
    await act(async () => {
      await expect(
        auth.changePassword({
          current_password: "old-password",
          new_password: "new-password-long",
          credential_version: "1",
        }),
      ).rejects.toBeInstanceOf(Error);
    });
    expect(auth.status).toBe("error");
    expect(container.textContent).toContain("Hasło zostało zmienione");
  } finally {
    await act(async () => root.unmount());
  }
});
