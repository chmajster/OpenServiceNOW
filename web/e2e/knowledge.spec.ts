import { expect, test, type Page } from "@playwright/test";
import { permanentRootPassword } from "./account-fixtures";

const tenant = process.env.E2E_TENANT ?? "e2e";
const adminEmail = process.env.E2E_ADMIN_EMAIL ?? "admin";

async function signIn(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Organizacja", { exact: true }).fill(tenant);
  await page.getByLabel("Login lub e-mail").fill(adminEmail);
  await page.getByLabel("Hasło", { exact: true }).fill(permanentRootPassword);
  const submit = async () => {
    const response = page.waitForResponse((item) => item.url().endsWith("/api/v1/auth/login") && item.request().method() === "POST");
    await page.getByRole("button", { name: "Zaloguj", exact: true }).click();
    return response;
  };
  let response = await submit();
  if (response.status() === 429) {
    const wait = Number(response.headers()["retry-after"]);
    expect(wait).toBeGreaterThan(0);
    await new Promise((resolve) => setTimeout(resolve, wait * 1000));
    response = await submit();
  }
  expect(response.status()).toBe(200);
}

test("documentation space supports the full Confluence-style workflow", async ({ page }, info) => {
  test.setTimeout(120_000);
  page.setDefaultTimeout(10_000);
  await signIn(page);

  await page.getByRole("link", { name: "Dokumentacja", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Dokumentacja", exact: true })).toBeVisible();
  const key = `${info.project.name === "mobile-chromium" ? "M" : "D"}${Date.now().toString().slice(-10)}`;
  const name = `Dokumentacja ${info.project.name} ${key}`;
  const createSpace = page.getByRole("heading", { name: "Nowa przestrzeń", exact: true });
  if (!(await createSpace.isVisible())) await page.getByRole("button", { name: "Nowa przestrzeń", exact: true }).click();
  await page.getByLabel("Klucz", { exact: true }).fill(key);
  await page.getByLabel("Nazwa", { exact: true }).fill(name);
  await page.getByLabel("Opis", { exact: true }).fill("Procedury i wiedza operacyjna zespołu");
  await page.getByLabel("Przegląd co ile dni", { exact: true }).fill("30");
  await page.getByRole("button", { name: "Utwórz przestrzeń", exact: true }).click();
  await page.getByRole("link", { name: new RegExp(name) }).click();
  await expect(page.getByRole("heading", { name, exact: true })).toBeVisible();

  await page.getByText("Więcej działań", { exact: true }).click();
  for (const label of ["Ustawienia przestrzeni", "Dokumenty wymagające uwagi", "Obserwuj przestrzeń", "Eksport ZIP", "Import"])
    await expect(page.getByRole("menuitem", { name: label, exact: true })).toBeVisible();
  await page.getByRole("menuitem", { name: "Obserwuj przestrzeń", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Obserwujesz tę przestrzeń" })).toBeVisible();

  await page.getByRole("button", { name: "Szablony", exact: true }).click();
  await page.getByRole("link", { name: "Użyj szablonu", exact: true }).first().click();
  const title = `Wdrożenie ${info.project.name}`;
  await page.getByLabel("Tytuł", { exact: true }).fill(title);
  await page.locator(".knowledge-editor-meta").getByRole("combobox").nth(1).selectOption("PUBLISHED");
  const yesterday = new Date(Date.now() - 86_400_000);
  const local = new Date(yesterday.getTime() - yesterday.getTimezoneOffset() * 60_000).toISOString().slice(0, 16);
  await page.getByLabel("Termin przeglądu", { exact: true }).fill(local);
  await page.getByRole("button", { name: "Zapisz stronę", exact: true }).click();
  await expect(page.getByRole("heading", { name: title, exact: true })).toBeVisible();
  await expect(page.getByText("Historia wersji", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: "Szukaj", exact: true }).click();
  await page.getByLabel("Przeszukaj tytuły i treść", { exact: true }).fill("bezpieczny sposób");
  await page.getByRole("button", { name: "Szukaj", exact: true }).last().click();
  await expect(page.getByRole("link", { name: new RegExp(title) })).toBeVisible();

  await page.getByText("Więcej działań", { exact: true }).click();
  await page.getByRole("menuitem", { name: "Dokumenty wymagające uwagi", exact: true }).click();
  await expect(page.getByText("Minął termin przeglądu", { exact: false })).toBeVisible();

  await page.getByText("Więcej działań", { exact: true }).click();
  const downloadEvent = page.waitForEvent("download");
  await page.getByRole("menuitem", { name: "Eksport ZIP", exact: true }).click();
  const archive = await downloadEvent;
  const archivePath = await archive.path();
  expect(archive.suggestedFilename()).toContain("documentation.zip");
  expect(archivePath).not.toBeNull();
  await page.locator('input[type="file"]').setInputFiles(archivePath!);
  await expect(page.getByRole("status").filter({ hasText: "Zaimportowano 1 stron" })).toBeVisible();

  await page.getByRole("button", { name: "Strony", exact: true }).click();
  await expect(page.getByRole("link", { name: new RegExp(title) })).toHaveCount(2);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});
