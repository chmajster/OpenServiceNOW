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

test("Knowledge Center supports the ServiceNow-style article lifecycle", async ({ page }, info) => {
  test.setTimeout(120_000);
  page.setDefaultTimeout(10_000);
  await signIn(page);

  await page.getByRole("link", { name: "Baza wiedzy", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Witamy w Bazie Wiedzy", exact: true })).toBeVisible();
  const key = `${info.project.name === "mobile-chromium" ? "M" : "D"}${Date.now().toString().slice(-10)}`;
  const name = `Wsparcie ${info.project.name} ${key}`;
  const createBase = page.getByRole("heading", { name: "Nowa baza wiedzy", exact: true });
  if (!(await createBase.isVisible())) await page.getByRole("button", { name: "Nowa baza wiedzy", exact: true }).click();
  await page.getByLabel("Identyfikator", { exact: true }).fill(key);
  await page.getByLabel("Nazwa", { exact: true }).fill(name);
  await page.getByLabel("Opis", { exact: true }).fill("Zweryfikowana wiedza dla zespołu wsparcia");
  await page.getByLabel("Domyślna ważność (dni)", { exact: true }).fill("30");
  await page.getByRole("button", { name: "Utwórz bazę wiedzy", exact: true }).click();
  await page.getByRole("link", { name: new RegExp(name) }).click();
  await expect(page.getByRole("heading", { name, exact: true })).toBeVisible();

  for (const label of ["Wszystkie", "Nieopublikowane", "Opublikowane", "Wycofane", "Wygasające", "Wyszukiwanie", "Konfiguracja bazy"])
    await expect(page.getByRole("button", { name: new RegExp(`^${label}`) })).toBeVisible();
  for (const removed of ["Więcej działań", "Eksport ZIP", "Import", "Szablony"])
    await expect(page.getByText(removed, { exact: true })).toHaveCount(0);

  await page.getByRole("button", { name: "Subskrybuj", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Subskrybujesz tę bazę wiedzy" })).toBeVisible();

  await page.getByRole("link", { name: "Nowy artykuł", exact: true }).click();
  const title = `Reset hasła ${info.project.name}`;
  await page.getByLabel("Krótki opis", { exact: true }).fill(title);
  await page.getByLabel("Treść artykułu", { exact: true }).fill("# Reset hasła\n\nZweryfikuj tożsamość użytkownika i bezpiecznie zresetuj hasło.");
  await page.getByRole("combobox", { name: "Stan", exact: true }).selectOption("PUBLISHED");
  const yesterday = new Date(Date.now() - 86_400_000);
  const local = new Date(yesterday.getTime() - yesterday.getTimezoneOffset() * 60_000).toISOString().slice(0, 16);
  await page.getByLabel("Ważny do", { exact: true }).fill(local);
  await page.getByRole("button", { name: "Zapisz artykuł", exact: true }).click();
  await expect(page.getByRole("heading", { name: title, exact: true })).toBeVisible();
  await expect(page.locator(".knowledge-article-number")).toHaveText(/^KB\d{7,}$/);
  await expect(page.getByText("Wersje artykułu", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: "Wyszukiwanie", exact: true }).click();
  await page.getByLabel("Wyszukaj w tytule i treści", { exact: true }).fill("bezpiecznie zresetuj");
  await page.getByRole("button", { name: "Szukaj", exact: true }).click();
  await expect(page.getByRole("link", { name: new RegExp(title) })).toBeVisible();

  await page.locator(".knowledge-module-home").click();
  const secondKey = `X${Date.now().toString().slice(-10)}`;
  const secondName = `Aplikacje ${info.project.name} ${secondKey}`;
  await page.getByRole("button", { name: "Nowa baza wiedzy", exact: true }).click();
  await page.getByLabel("Identyfikator", { exact: true }).fill(secondKey);
  await page.getByLabel("Nazwa", { exact: true }).fill(secondName);
  await page.getByLabel("Domyślna ważność (dni)", { exact: true }).fill("30");
  await page.getByRole("button", { name: "Utwórz bazę wiedzy", exact: true }).click();
  await page.getByRole("link", { name: new RegExp(secondName) }).click();
  await page.getByRole("link", { name: "Nowy artykuł", exact: true }).click();
  const crossBaseTitle = `Konfiguracja VPN ${info.project.name}`;
  await page.getByLabel("Krótki opis", { exact: true }).fill(crossBaseTitle);
  await page.getByLabel("Treść artykułu", { exact: true }).fill("vpn-cross-base-search-proof");
  await page.getByRole("combobox", { name: "Stan", exact: true }).selectOption("PUBLISHED");
  await page.getByRole("button", { name: "Zapisz artykuł", exact: true }).click();
  await expect(page.getByRole("heading", { name: crossBaseTitle, exact: true })).toBeVisible();
  await page.locator(".knowledge-module-home").click();
  await page.getByLabel("Wyszukaj artykuły we wszystkich bazach", { exact: true }).fill("vpn-cross-base-search-proof");
  await page.getByRole("button", { name: "Szukaj", exact: true }).click();
  const crossBaseResult = page.getByRole("link", { name: new RegExp(`${secondName}.*${crossBaseTitle}`) });
  await expect(crossBaseResult).toBeVisible();
  await expect(crossBaseResult).toHaveAttribute("href", /\/knowledge\/\d+\/articles\/\d+/);
  await page.getByRole("link", { name: "Wszystkie bazy", exact: true }).click();
  await page.getByRole("link", { name: new RegExp(name) }).click();

  await page.getByRole("button", { name: "Wygasające", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Wygasające artykuły", exact: true })).toBeVisible();
  await page.getByRole("link", { name: title, exact: true }).click();
  await page.getByRole("button", { name: "Edytuj artykuł", exact: true }).click();
  await page.getByRole("combobox", { name: "Stan", exact: true }).selectOption("RETIRED");
  await page.getByRole("button", { name: "Zapisz artykuł", exact: true }).click();
  await expect(page.getByText("Wycofany", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: /^Wycofane/ }).click();
  await expect(page.getByRole("link", { name: title, exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});
