import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, api, readCookie } from "./client";

afterEach(() => {
  document.cookie = "opensnow_csrf=; Max-Age=0; Path=/";
  vi.unstubAllGlobals();
});

describe("api client", () => {
  it("reads encoded cookies", () => {
    document.cookie = "opensnow_csrf=abc%20123; Path=/";
    expect(readCookie("opensnow_csrf")).toBe("abc 123");
  });

  it("adds CSRF header to unsafe requests", async () => {
    document.cookie = "opensnow_csrf=csrf-token; Path=/";
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      const headers = new Headers(init?.headers);
      expect(headers.get("X-CSRF-Token")).toBe("csrf-token");
      expect(init?.credentials).toBe("include");
      return new Response(null, { status: 204 });
    });
    vi.stubGlobal("fetch", fetchMock);

    await api<void>("/api/v1/auth/logout", { method: "POST" });
    expect(fetchMock).toHaveBeenCalledOnce();
  });

  it("maps JSON errors to ApiError", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(
          JSON.stringify({
            error: {
              code: "FORBIDDEN",
              message: "Permission denied",
              request_id: "req-1"
            }
          }),
          {
            status: 403,
            headers: { "Content-Type": "application/json" }
          }
        )
      )
    );

    await expect(api("/api/v1/admin/rbac/roles")).rejects.toEqual(
      expect.objectContaining({
        status: 403,
        code: "FORBIDDEN",
        requestId: "req-1"
      })
    );
  });
});
