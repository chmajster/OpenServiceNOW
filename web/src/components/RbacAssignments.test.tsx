import { act } from "react";
import { createRoot } from "react-dom/client";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { AuthProvider } from "../auth/AuthContext";
import { GroupMembers } from "./RbacAssignments";

afterEach(() => vi.unstubAllGlobals());

it("links each group member to their user profile", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal("fetch", vi.fn(async (path: string) => {
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
        : path === "/api/v1/admin/rbac/groups/5/members?limit=50"
          ? {
              members: [{
                user_id: "8",
                email: "operator@example.test",
                display_name: "Jan Operator",
                user_active: true,
                membership_active: true,
                joined_at: "2026-02-01T10:00:00Z",
              }],
            }
          : { users: [] };
    return new Response(JSON.stringify(payload), { headers: { "Content-Type": "application/json" } });
  }));

  const container = document.createElement("div");
  const root = createRoot(container);
  try {
    await act(async () => root.render(
      <MemoryRouter>
        <AuthProvider>
          <GroupMembers groupID="5" onChanged={() => undefined} />
        </AuthProvider>
      </MemoryRouter>,
    ));
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });

    expect(container.textContent).toContain("Jan Operator");
    expect(container.querySelector('a[href="/users/8"]')?.textContent).toBe("Jan Operator");
  } finally {
    await act(async () => root.unmount());
  }
});
