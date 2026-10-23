import { expect, test } from "@playwright/test";

const ci = {
  id: "72",
  version: "1",
  identifier: "vpn-gw-ci",
  name: "VPN gateway CI",
  class: "VM",
  status: "ACTIVE",
  owner_id: "7",
  owner_name: "Ala Kowalska",
  environment: "PROD",
  location: "Warszawa",
  attributes: {},
  archived: false,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

test("CMDB list, URL-backed search, detail and locale switch work on desktop and mobile", async ({ page }) => {
  const requests: string[] = [];
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname + url.search;
    requests.push(path);
    let payload: unknown;
    let status = 200;
    if (path === "/api/v1/setup") {
      payload = { required: false };
    } else if (path === "/api/v1/auth/me") {
      payload = {
        user: { id: "7", email: "ala@example.test", display_name: "Ala Kowalska", version: "1", credential_version: "1" },
        tenant: { id: "2", name: "Example tenant", slug: "example" },
        permissions: ["cmdb.read", "cmdb.manage", "cmdb.import"],
        password_change_required: false,
        expires_at: new Date(Date.now() + 600_000).toISOString(),
      };
    } else if (path === "/api/v1/cmdb/classes") {
      payload = {
        classes: [
          { key: "VM", label: "Virtual machine", attributes: [] },
          { key: "SERVICE", label: "Service", attributes: [] },
        ],
        ci_fields: [],
        statuses: ["ACTIVE", "MAINTENANCE"],
        environments: ["PROD", "TEST"],
        relation_types: [],
        ci_import_fields: [],
        ci_import_required_fields: [],
        ci_import_identity_keys: [],
        asset_import_fields: [],
        asset_import_required_fields: [],
        asset_import_identity_keys: [],
        max_csv_bytes: 1_048_576,
        max_csv_rows: 500,
        max_csv_columns: 40,
      };
    } else if (url.pathname === "/api/v1/cmdb/cis") {
      payload = { items: [ci], next_before_id: "" };
    } else if (url.pathname === "/api/v1/cmdb/cis/72") {
      payload = ci;
    } else {
      status = 404;
      payload = { error: { code: "NOT_FOUND", message: "Not found" } };
    }
    await route.fulfill({
      status,
      contentType: "application/json",
      body: JSON.stringify(payload),
    });
  });

  await page.goto("/cmdb");
  await expect(page.getByRole("heading", { name: "Elementy konfiguracji (CMDB)" })).toBeVisible();
  await expect(page.getByText("VPN gateway CI")).toBeVisible();
  await expect.poll(() => requests.some((path) => path.startsWith("/api/v1/cmdb/cis?archived=false"))).toBe(true);

  await page.getByLabel("Szukaj po nazwie lub identyfikatorze").fill("vpn");
  await page.getByRole("button", { name: "Zastosuj filtry" }).click();
  await expect(page).toHaveURL(/q=vpn/);
  await expect.poll(() => requests.some((path) => path.includes("q=vpn"))).toBe(true);

  await page.getByRole("link", { name: "vpn-gw-ci" }).click();
  await expect(page).toHaveURL(/\/cmdb\/72/);
  await expect(page.getByRole("heading", { name: "VPN gateway CI" })).toBeVisible();
  await page.getByRole("button", { name: "Zmień język CMDB na angielski" }).click();
  await expect(page.getByRole("tab", { name: "Overview" })).toBeVisible();
  await expect.poll(() => page.evaluate(() => document.documentElement.lang)).toBe("en");
  await page.reload();
  await expect(page.getByRole("heading", { name: "VPN gateway CI" })).toBeVisible();
  await expect(page.getByRole("tab", { name: "Overview" })).toBeVisible();
  await expect.poll(() => page.evaluate(() => document.documentElement.lang)).toBe("en");
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});
