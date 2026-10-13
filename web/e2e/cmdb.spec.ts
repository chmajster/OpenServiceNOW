import { expect, test, type Page } from "@playwright/test";
import { loginAPI, permanentRootPassword } from "./account-fixtures";

async function choose(page: Page, label: string, value: string) {
  const input = page.getByRole("combobox", { name: label, exact: true });
  await input.fill(value);
  await expect(
    page.getByRole("option", { name: value, exact: true }),
  ).toBeVisible();
  await input.press("ArrowDown");
  await input.press("Enter");
}
test("CMDB has typed classes, preserved drafts, directed dependencies and real incident links", async ({
  page,
}, info) => {
  test.setTimeout(150000);
  page.setDefaultTimeout(10000);
  expect(
    (
      await loginAPI(page.request, {
        tenant: process.env.E2E_TENANT ?? "e2e",
        email: process.env.E2E_ADMIN_EMAIL ?? "admin",
        password: permanentRootPassword,
      })
    ).status(),
  ).toBe(200);
  const headers = {
    "X-CSRF-Token": (await page.request.storageState()).cookies.find(
      (c) => c.name === "opensnow_csrf",
    )!.value,
  };
  const suffix = `${info.project.name}-${Date.now()}`,
    groupName = `Wsparcie ${suffix}`;
  const group = await page.request.post("/api/v1/admin/rbac/groups", {
    headers,
    data: { name: groupName },
  });
  expect(group.status()).toBe(201);
  const groupID = (await group.json()).id;
  await page.goto("/cmdb/new");
  await page.getByLabel("Klasa CI", { exact: true }).selectOption("DATABASE");
  await page
    .getByLabel("Identyfikator CI", { exact: true })
    .fill(`db-${suffix}`);
  await page.getByLabel("Nazwa CI", { exact: true }).fill(`Baza ${suffix}`);
  await page
    .getByLabel("Stan operacyjny", { exact: true })
    .selectOption("ACTIVE");
  await page
    .getByLabel("Etap cyklu życia", { exact: true })
    .selectOption("OPERATE");
  await page
    .getByLabel("Krytyczność", { exact: true })
    .selectOption("CRITICAL");
  await choose(page, "Grupa wsparcia", groupName);
  await page.getByLabel("Silnik bazy", { exact: true }).fill("PostgreSQL");
  await page.getByLabel("Port", { exact: true }).fill("5432");
  await page.getByLabel("Hostname", { exact: true }).fill("db.example.test");
  let lost = false;
  await page.route("**/api/v1/cmdb/cis", async (route) => {
    if (route.request().method() === "POST" && !lost) {
      lost = true;
      await route.fetch();
      await route.abort("failed");
    } else await route.continue();
  });
  await page.getByRole("button", { name: "Utwórz CI", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Potwierdź wynik zapisu", exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("Nazwa CI", { exact: true })).toHaveValue(
    `Baza ${suffix}`,
  );
  await page
    .getByRole("button", { name: "Potwierdź wynik zapisu", exact: true })
    .click();
  await expect(page).toHaveURL(/\/cmdb\/[0-9]+$/);
  await page.unroute("**/api/v1/cmdb/cis");
  const dbID = new URL(page.url()).pathname.split("/").at(-1)!;
  if(process.env.E2E_ASSERT_BIGINT) expect(BigInt(dbID)).toBeGreaterThan(9007199254740991n);
  const db = await (await page.request.get(`/api/v1/cmdb/cis/${dbID}`)).json();
  expect(db.attributes.port).toBe(5432);
  expect(db.support_group_id).toBe(groupID);
  const exact = await (
    await page.request.get(`/api/v1/cmdb/cis?q=db-${suffix}`)
  ).json();
  expect(exact.total).toBe("1");
  await page
    .getByLabel("Nazwa CI", { exact: true })
    .fill(`Moja propozycja ${suffix}`);
  const {
    id,
    version,
    owner_name,
    support_group_name,
    archived,
    created_at,
    updated_at,
    ...fields
  } = db;
  void id;
  void owner_name;
  void support_group_name;
  void archived;
  void created_at;
  void updated_at;
  expect(
    (
      await page.request.put(`/api/v1/cmdb/cis/${dbID}`, {
        headers,
        data: { ...fields, version, name: `Zdalna ${suffix}` },
      })
    ).status(),
  ).toBe(200);
  await page.getByRole("button", { name: "Odśwież CI", exact: true }).click();
  await expect(page.getByLabel("Nazwa CI", { exact: true })).toHaveValue(
    `Moja propozycja ${suffix}`,
  );
  await page.getByRole("button", { name: "Zapisz CI", exact: true }).click();
  await expect(
    page.getByRole("region", { name: "Konflikt wersji" }),
  ).toContainText(`Zdalna ${suffix}`);
  await page
    .getByRole("button", {
      name: "Zachowaj propozycję i użyj aktualnej wersji",
      exact: true,
    })
    .click();
  await page.getByRole("button", { name: "Zapisz CI", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    `Moja propozycja ${suffix}`,
  );
  if (info.project.name === "chromium") {
    for (const width of [1366, 1920]) {
      await page.setViewportSize({ width, height: 900 });
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      ).toBe(true);
      await page.evaluate(()=>{(document.activeElement as HTMLElement)?.blur();window.scrollTo(0,0)});
      await page.screenshot({
        path: info.outputPath(`ci-form-${width}.png`),
        fullPage: true,
      });
    }
  } else {
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.evaluate(()=>{(document.activeElement as HTMLElement)?.blur();window.scrollTo(0,0)});
      await page.screenshot({
      path: info.outputPath("ci-form-mobile.png"),
      fullPage: true,
    });
  }
  await page.getByRole("tab", { name: "Szczegóły", exact: true }).focus();
  await page.keyboard.press("ArrowRight");
  await expect(
    page.getByRole("tab", { name: "Zależności", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Home");
  await page.goto("/cmdb/new");
  await page.getByLabel("Klasa CI", { exact: true }).selectOption("SERVICE");
  await page
    .getByLabel("Identyfikator CI", { exact: true })
    .fill(`service-${suffix}`);
  await page.getByLabel("Nazwa CI", { exact: true }).fill(`Usługa ${suffix}`);
  await page.getByRole("button", { name: "Utwórz CI", exact: true }).click();
  await expect(page).toHaveURL(/\/cmdb\/[0-9]+$/);
  const serviceID = new URL(page.url()).pathname.split("/").at(-1)!;
  await page.getByRole("tab", { name: "Zależności", exact: true }).click();
  await choose(page, "Docelowy CI", `Moja propozycja ${suffix}`);
  await page
    .getByLabel("Typ zależności", { exact: true })
    .selectOption("DEPENDS_ON");
  await page
    .getByRole("button", { name: "Dodaj zależność", exact: true })
    .click();
  await expect(page.locator("table")).toContainText(
    `Moja propozycja ${suffix}`,
  );
  const graph = await (
    await page.request.get(`/api/v1/cmdb/cis/${serviceID}/graph`)
  ).json();
  expect(graph.nodes).toHaveLength(2);
  expect(graph.edges[0].source_id).toBe(serviceID);
  expect(graph.edges[0].target_id).toBe(dbID);
  const incident = await page.request.post("/api/v1/incidents/", {
    headers: { ...headers, "Idempotency-Key": crypto.randomUUID() },
    data: {
      title: `CMDB incydent ${suffix}`,
      description: "Problem działania bazy",
      category: "software",
      subcategory: "application",
      impact: 2,
      urgency: 2,
    },
  });
  expect(incident.status()).toBe(201);
  const inc = await incident.json();
  await page.goto(`/incidents/${inc.id}?tab=cmdb`);
  await choose(page, "Element konfiguracji", `Moja propozycja ${suffix}`);
  await page.getByRole("button", { name: "Powiąż CI", exact: true }).click();
  await expect(
    page
      .getByRole("link", { name: new RegExp(`Moja propozycja ${suffix}`) })
      .first(),
  ).toBeVisible();
  await page.goto(`/cmdb/${dbID}?tab=records`);
  await expect(
    page.getByRole("link", { name: new RegExp(inc.number) }),
  ).toBeVisible();
  await page.getByRole("tab", { name: "Szczegóły", exact: true }).click();
  await page
    .getByLabel("Uzasadnienie archiwizacji lub przywrócenia")
    .fill("Wycofanie w teście nie usuwa powiązań");
  await page
    .getByRole("button", { name: "Archiwizuj CI", exact: true })
    .click();
  await expect(page.locator(".record-facts")).toContainText("Archiwum");
  await page.reload();
  await expect(page.locator(".record-facts")).toContainText("Archiwum");
  await page
    .getByRole("tab", { name: "Powiązane rekordy", exact: true })
    .click();
  await expect(
    page.getByRole("link", { name: new RegExp(inc.number) }),
  ).toBeVisible();
  await page.getByRole("tab", { name: "Zależności", exact: true }).click();
  await expect(
    page.getByRole("link", { name: `Usługa ${suffix}`, exact: true }).first(),
  ).toBeVisible();
  if (info.project.name === "chromium") {
    for (const width of [1366, 1920]) {
      await page.setViewportSize({ width, height: 900 });
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      ).toBe(true);
      await page.evaluate(()=>{(document.activeElement as HTMLElement)?.blur();window.scrollTo(0,0)});
      await page.screenshot({
        path: info.outputPath(`ci-${width}.png`),
        fullPage: true,
      });
    }
  } else {
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.evaluate(()=>{(document.activeElement as HTMLElement)?.blur();window.scrollTo(0,0)});
      await page.screenshot({
      path: info.outputPath("ci-mobile.png"),
      fullPage: true,
    });
  }
  await page.goto(`/cmdb?q=${encodeURIComponent(suffix)}&limit=1`);
  await expect(
    page.getByRole("region", { name: "Konfiguracja listy" }),
  ).toContainText("Dostępnych rekordów: 2");
  await page
    .getByRole("button", { name: "Następna strona", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Pierwsza strona", exact: true }),
  ).toBeVisible();
});
