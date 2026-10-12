import { act } from "react";
import { createRoot } from "react-dom/client";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { AuthProvider } from "../auth/AuthContext";
import { NotificationAdminPage } from "./NotificationAdminPage";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
it("retains an SMTP secret through 409, masks conflict details and requires an explicit retry decision", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const secret = "private-SMTP-proposal";
  let config = {
    provider_id: "test",
    enabled: false,
    host: "localhost",
    port: 587,
    tls_mode: "STARTTLS",
    username: "",
    from_address: "sender@example.test",
    has_password: false,
    encryption_ready: true,
    insecure_allowed: false,
    version: "1",
  };
  const proposals: Record<string, unknown>[] = [];
  const persist = vi.spyOn(Storage.prototype, "setItem");
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        proposals.push(JSON.parse(init.body as string));
        if (proposals.length === 1) {
          config = { ...config, host: "concurrent.example.test", version: "2" };
          return new Response(
            JSON.stringify({
              error: { code: "VERSION_CONFLICT", message: "Conflict" },
            }),
            { status: 409, headers: { "Content-Type": "application/json" } },
          );
        }
        config = { ...config, version: "3", has_password: true };
        return new Response(null, { status: 204 });
      }
      const payload =
        path === "/api/v1/setup"
          ? { required: false }
          : path === "/api/v1/auth/me"
            ? {
                user: {
                  id: "1",
                  email: "admin@example.test",
                  display_name: "Admin",
                  version: "1",
                  credential_version: "1",
                },
                tenant: { id: "2", name: "Org", slug: "org" },
                permissions: ["admin.notifications.manage"],
                password_change_required: false,
                expires_at: new Date(Date.now() + 600000).toISOString(),
              }
            : config;
      return new Response(JSON.stringify(payload), {
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
  const container = document.createElement("div"),
    root = createRoot(container);
  const router = createMemoryRouter([
    {
      path: "/",
      element: (
        <AuthProvider>
          <NotificationAdminPage />
        </AuthProvider>
      ),
    },
  ]);
  const button = (text: string) =>
    [...container.querySelectorAll("button")].find(
      (b) => b.textContent === text,
    )!;
  try {
    await act(async () => root.render(<RouterProvider router={router} />));
    const toggle = [...container.querySelectorAll("label")]
      .find((l) => l.textContent === "Zmień lub usuń zapisane hasło")!
      .querySelector<HTMLInputElement>("input")!;
    await act(async () => toggle.click());
    const password = container.querySelector<HTMLInputElement>(
      'input[type="password"]',
    )!;
    await act(async () => {
      Object.getOwnPropertyDescriptor(
        HTMLInputElement.prototype,
        "value",
      )!.set!.call(password, secret);
      password.dispatchEvent(new Event("input", { bubbles: true }));
    });
    const submit = () =>
      container
        .querySelector("form")!
        .dispatchEvent(
          new Event("submit", { bubbles: true, cancelable: true }),
        );
    await act(async () => {
      submit();
    });
    expect(container.textContent).toContain("Konflikt wersji");
    expect(container.textContent).not.toContain(secret);
    expect(password.value).toBe(secret);
    expect(button("Zapisz konfigurację").disabled).toBe(true);
    await act(async () =>
      button("Zachowaj propozycję do ponownego zapisu").click(),
    );
    await act(async () => {
      submit();
    });
    expect(proposals[1]).toMatchObject({
      password: secret,
      version: "2",
      host: "localhost",
    });
    expect(container.textContent).toContain("Zapisano konfigurację SMTP");
    expect(container.querySelector('input[type="password"]')).toBeNull();
    expect(persist).not.toHaveBeenCalled();
  } finally {
    await act(async () => root.unmount());
    router.dispose();
  }
});
