import { expect, test } from "@playwright/test";
import { loginAPI, permanentRootPassword } from "./account-fixtures";

test("operator workspace connects real queues, search, tabs and session context", async ({
  page,
}, info) => {
  test.setTimeout(120_000);
  page.setDefaultTimeout(10_000);
  expect(
    (
      await loginAPI(page.request, {
        tenant: process.env.E2E_TENANT ?? "e2e",
        email: process.env.E2E_ADMIN_EMAIL ?? "admin",
        password: permanentRootPassword,
      })
    ).status(),
  ).toBe(200);
  const me = await (await page.request.get("/api/v1/auth/me")).json();
  const headers = {
    "X-CSRF-Token": (await page.request.storageState()).cookies.find(
      (c) => c.name === "opensnow_csrf",
    )!.value,
  };
  const records: { id: string; number: string; title: string }[] = [];
  for (let i = 0; i < 2; i++) {
    const title = `Workspace ${info.project.name} ${Date.now()} ${i}`;
    const created = await page.request.post("/api/v1/incidents/", {
      headers: { ...headers, "Idempotency-Key": crypto.randomUUID() },
      data: {
        title,
        description: "Sprawdzenie powrotu do rzeczywistej kolejki",
        category: "other",
        subcategory: "other",
        impact: 3,
        urgency: 3,
      },
    });
    expect(created.status()).toBe(201);
    const record = await created.json();
    records.push(record);
    expect(
      (
        await page.request.put(`/api/v1/incidents/${record.id}/assignment`, {
          headers,
          data: {
            version: record.version,
            assigned_to: me.user.id,
            assignment_group_id: null,
          },
        })
      ).status(),
    ).toBe(200);
  }
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Mój obszar pracy", exact: true }),
  ).toBeVisible();
  if (info.project.name === "mobile-chromium") {
    expect(
      await page.evaluate(() => matchMedia("(pointer: coarse)").matches),
    ).toBe(true);
    const size = await page
      .getByRole("searchbox", { name: "Wyszukaj rekordy" })
      .boundingBox();
    expect(size?.height).toBeGreaterThanOrEqual(44);
  }
  const summary = await (
    await page.request.get("/api/v1/workspace/summary")
  ).json();
  const mine = summary.metrics.find(
    (m: { key: string }) => m.key === "my_incidents",
  );
  expect(BigInt(mine.count)).toBeGreaterThanOrEqual(2n);
  await page
    .getByRole("link", {
      name: `Moje aktywne incydenty: ${mine.count}`,
      exact: true,
    })
    .click();
  await expect(page).toHaveURL(/active=true&view=assigned/);
  await expect(page.getByLabel("Tylko aktywne")).toBeChecked();
  await page
    .locator(".table-scroll")
    .getByRole("link", { name: records[0]!.number, exact: true })
    .click();
  const tabs = page.getByRole("navigation", { name: "Otwarte rekordy" });
  await expect(
    tabs.getByRole("link", { name: records[0]!.number, exact: true }),
  ).toHaveCount(1);
  await page.getByRole("button", { name: "Kontekst", exact: true }).click();
  const context = page.getByRole("complementary", { name: "Kontekst pracy" });
  await expect(context).toContainText(records[0]!.title);
  await context.getByRole("button", { name: "Dodaj do ulubionych" }).click();
  await context.getByRole("button", { name: "Ulubione", exact: true }).click();
  await expect(
    context.getByRole("link", { name: new RegExp(records[0]!.number) }),
  ).toBeVisible();
  await context.getByRole("button", { name: "Zamknij kontekst" }).click();
  const search = page.getByRole("search");
  await search.getByLabel("Wyszukaj rekordy").fill(records[1]!.number);
  await search.getByRole("button", { name: "Szukaj", exact: true }).click();
  const results = page.getByRole("region", { name: "Wyniki wyszukiwania" });
  await expect(
    results.getByText("Dokładny numer", { exact: true }),
  ).toBeVisible();
  await results.getByRole("link").click();
  await expect(tabs.locator(".workspace-tab")).toHaveCount(2);
  await tabs
    .getByRole("link", { name: records[0]!.number, exact: true })
    .click();
  await expect(tabs.locator(".workspace-tab")).toHaveCount(2);
  await page.getByRole("link", { name: "Wróć do listy", exact: true }).click();
  await expect(page).toHaveURL(/active=true&view=assigned/);
  await tabs
    .getByRole("link", { name: records[1]!.number, exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: records[1]!.title, exact: true }),
  ).toBeVisible();
  await page.goBack();
  await expect(page).toHaveURL(/active=true&view=assigned/);
  await page.goForward();
  await expect(
    page.getByRole("heading", { name: records[1]!.title, exact: true }),
  ).toBeVisible();
  const second = await page.context().newPage();
  await second.goto(page.url());
  await expect(
    second.getByRole("heading", { name: records[1]!.title, exact: true }),
  ).toBeVisible();
  await second.close();
  await page.reload();
  await expect(
    tabs.getByRole("link", { name: records[1]!.number, exact: true }),
  ).toHaveCount(1);
  await page
    .getByRole("link", { name: "Komunikacja i pliki", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Notatka wewnętrzna", exact: true })
    .click();
  await page
    .getByLabel("Treść notatki wewnętrznej")
    .fill("Poufny niezapisany szkic");
  await tabs
    .getByRole("button", { name: `Zamknij ${records[1]!.number}` })
    .click();
  await expect(page.getByRole("alertdialog")).toContainText(
    "Niezapisana praca",
  );
  await page.getByRole("button", { name: "Pozostań na stronie" }).click();
  await expect(page.getByLabel("Treść notatki wewnętrznej")).toHaveValue(
    "Poufny niezapisany szkic",
  );
  expect(
    await page.evaluate(() =>
      Object.values(localStorage).some((v) =>
        String(v).includes("Poufny niezapisany szkic"),
      ),
    ),
  ).toBe(false);
  await page.getByLabel("Treść notatki wewnętrznej").fill("");
  await page.getByRole("link", { name: "Informacje", exact: true }).click();
  for (const width of info.project.name === "mobile-chromium"
    ? [412]
    : [1366, 1920]) {
    await page.setViewportSize({ width, height: width === 412 ? 915 : 900 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    const path = info.outputPath(`workspace-${width}.png`);
    await page.screenshot({ path, fullPage: true });
    await info.attach(`workspace-${width}`, { path, contentType: "image/png" });
  }
  await page.locator(".user-menu summary").click();
  await page.getByRole("button", { name: "Wyloguj", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Zaloguj się" }),
  ).toBeVisible();
  await expect(tabs).toHaveCount(0);
  expect(
    await page.evaluate(() =>
      Object.keys(sessionStorage).some((k) => k.startsWith("opensnow-scroll:")),
    ),
  ).toBe(false);
});
