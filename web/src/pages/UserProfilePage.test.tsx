import { act } from "react";
import { createRoot } from "react-dom/client";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { AuthProvider } from "../auth/AuthContext";
import { UserProfilePage } from "./UserProfilePage";

afterEach(() => vi.unstubAllGlobals());

it("loads and displays a tenant user profile without credential data", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const requests: string[] = [];
  vi.stubGlobal("fetch", vi.fn(async (path: string) => {
    requests.push(path);
    const payload = path === "/api/v1/setup"
      ? { required: false }
      : path === "/api/v1/auth/me"
        ? {
            user: { id: "7", email: "admin@example.test", display_name: "Administrator", version: "1", credential_version: "1" },
            tenant: { id: "2", name: "Firma", slug: "firma" },
            permissions: ["admin.users.manage"],
            password_change_required: false,
            expires_at: new Date(Date.now() + 600_000).toISOString(),
          }
        : {
            profile: {
              id: "8",
              email: "operator@example.test",
              display_name: "Jan Operator",
              user_active: true,
              membership_active: false,
              created_at: "2026-01-01T10:00:00Z",
              membership_created_at: "2026-02-01T10:00:00Z",
            },
          };
    return new Response(JSON.stringify(payload), { headers: { "Content-Type": "application/json" } });
  }));

  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () => root.render(
      <MemoryRouter initialEntries={["/users/8"]}>
        <AuthProvider>
          <Routes>
            <Route path="/users/:userID" element={<UserProfilePage />} />
          </Routes>
        </AuthProvider>
      </MemoryRouter>,
    ));
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });

    expect(container.textContent).toContain("Jan Operator");
    expect(container.textContent).toContain("operator@example.test");
    expect(container.textContent).toContain("Dołączono do organizacji");
    expect(container.textContent).toContain("Członkostwo wyłączone");
    expect(container.textContent).toContain("Wyłączone");
    expect(container.querySelector('a[href="/account"]')).toBeNull();
    expect(requests).toContain("/api/v1/admin/users/8");
  } finally {
    await act(async () => root.unmount());
  }
});
