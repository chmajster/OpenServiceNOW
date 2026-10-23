import { expect, test } from "@playwright/test";

test("service portfolio creates a draft and renders its bounded CSDM detail", async ({ page }) => {
  const state: { created?: Record<string, any>; postedItem?: Record<string, any>; createKey: string } = { createKey: "" };
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;
    let payload: unknown = {};
    let status = 200;
    if (path === "/api/v1/setup") {
      payload = { required: false };
    } else if (path === "/api/v1/auth/me") {
      payload = {
        user: { id: "7", email: "ala@example.test", display_name: "Ala Kowalska", version: "1", credential_version: "1" },
        tenant: { id: "2", name: "Example tenant", slug: "example" },
        permissions: ["service_portfolio.read", "service_portfolio.manage", "service_portfolio.offer.read", "catalog.read", "cmdb.read", "incident.read", "request.read_own"],
        password_change_required: false,
        expires_at: new Date(Date.now() + 600_000).toISOString(),
      };
    } else if (path === "/api/v1/metadata/preferences") {
      payload = { locale: "pl", time_zone: "UTC", version: "1", organization_name: "Example tenant", brand_color: "#0F766E", default_locale: "pl", default_time_zone: "UTC", default_date_format: "YYYY-MM-DD", default_number_format: "1,234.56", default_currency: "USD" };
    } else if (path === "/api/v1/service-portfolio/items" && route.request().method() === "GET") {
      payload = { items: state.created ? [state.created] : [], next_before_id: "" };
    } else if (path === "/api/v1/service-portfolio/items" && route.request().method() === "POST") {
      state.postedItem = route.request().postDataJSON() as Record<string, any>;
      state.createKey = route.request().headers()["idempotency-key"] ?? "";
      state.created = {
        id: "72", service_key: state.postedItem.service_key, name: state.postedItem.name, type: state.postedItem.type,
        description: state.postedItem.description, lifecycle: "DRAFT", criticality: state.postedItem.criticality,
        environment: state.postedItem.environment, scope: state.postedItem.scope, business_owner_id: "0", business_owner_name: "",
        technical_owner_id: "0", technical_owner_name: "", support_group_id: "0", support_group_name: "",
        org_unit_ref_id: "0", org_unit_name: "", department_ref_id: "0", department_name: "", cost_center_ref_id: "0",
        cost_center_name: "", location_id: "0", location_name: "", ci_id: "0", ci_name: "", ci_class: "",
        ci_version: "0", dependency_mode: state.postedItem.dependency_mode, dependency_quorum: 0, version: "1", configuration_version: "1",
      };
      payload = state.created;
    } else if (path === "/api/v1/service-portfolio/items/72") {
      payload = state.created ?? {};
    } else if (path === "/api/v1/service-portfolio/items/72/relations") {
      payload = { items: [], next_before_id: "" };
    } else if (path === "/api/v1/service-portfolio/items/72/impact") {
      payload = { root: { id: "service:72", kind: "SERVICE", name: state.created?.name ?? "Customer Portal", depth: 0, availability: "NOT_MODELED", availability_rule: "no_redundancy_model" }, nodes: [], edges: [], impacted_services: [], impacted_offers: [], rules_used: ["no_redundancy_model"], truncated: false, max_nodes: 100, max_depth: 6, rule_version: "1", calculated_at: new Date().toISOString() };
    } else if (path === "/api/v1/service-portfolio/availability/ITEM/72") {
      payload = { node_kind: "ITEM", node_id: "72", state: "UNKNOWN", source: "NONE", note: "", version: "0" };
    } else if (path === "/api/v1/service-portfolio/offers") {
      payload = { items: [], next_before_id: "" };
    } else {
      status = 404;
      payload = { error: { code: "NOT_FOUND", message: "Not found" } };
    }
    await route.fulfill({ status, contentType: "application/json", body: JSON.stringify(payload) });
  });

  await page.goto("/service-portfolio");
  await expect(page.getByRole("heading", { name: "Portfel usług i model CSDM" })).toBeVisible();
  await page.getByRole("button", { name: "Nowa pozycja" }).click();
  await page.getByLabel("Stabilny klucz").fill("CUSTOMER.PORTAL");
  await page.getByLabel("Nazwa", { exact: true }).fill("Customer Portal");
  await page.getByLabel("Opis").fill("Customer facing portal service");
  await page.getByRole("button", { name: "Zapisz wersję roboczą" }).click();

  await expect(page).toHaveURL(/\/service-portfolio\/72$/);
  await expect(page.getByRole("heading", { name: "Customer Portal" })).toBeVisible();
  await expect(page.getByText("Graf ograniczony: 0/100 węzłów, głębokość 6, zestaw reguł 1.")).toBeVisible();
  expect(state.postedItem?.service_key).toBe("CUSTOMER.PORTAL");
  expect(state.postedItem?.version).toBe("0");
  expect(state.postedItem?.clear_ci_link).toBe(false);
  expect(state.createKey.length).toBeGreaterThanOrEqual(8);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});
