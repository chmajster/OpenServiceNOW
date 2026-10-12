import { expect, test, type Page } from "@playwright/test";
import { permanentRootPassword, changeTemporaryPassword } from "./account-fixtures";

const tenant = process.env.E2E_TENANT ?? "e2e";
const temporary = "account-temporary-password";
const next = "😀".repeat(12);
async function csrf(page: Page) { return { "X-CSRF-Token": (await page.context().cookies()).find(cookie => cookie.name === "opensnow_csrf")!.value }; }
async function signIn(page: Page, email: string, password: string) {
  await page.goto("/login");
  await page.getByLabel("Organizacja", { exact: true }).fill(tenant);
  await page.getByLabel("Login lub e-mail").fill(email);
  await page.getByLabel("Hasło", { exact: true }).fill(password);
  const response = page.waitForResponse(result => result.url().endsWith("/api/v1/auth/login") && result.request().method() === "POST");
  await page.getByRole("button", { name: "Zaloguj", exact: true }).click();
  expect((await response).status()).toBe(200);
  await expect(page).not.toHaveURL(/\/login$/);
}

test("own account enforces temporary passwords, keeps conflicts and revokes sessions", async ({ page, browser, request }, info) => {
  test.setTimeout(90_000);
  const email = `own-account-${info.project.name}-${info.retry}@example.test`;
  expect((await request.post("/api/v1/auth/login", { data: { tenant, email: process.env.E2E_ADMIN_EMAIL ?? "admin", password: permanentRootPassword } })).status()).toBe(200);
  const headers = { "X-CSRF-Token": (await request.storageState()).cookies.find(cookie => cookie.name === "opensnow_csrf")!.value };
  expect((await request.post("/api/v1/admin/users/", { headers, data: { email, display_name: "Moje konto test", password: temporary } })).status()).toBe(201);
  await signIn(page, email, temporary);
  await expect(page.getByText("Wymagana zmiana hasła", { exact: true })).toBeVisible();
  expect((await page.request.get("/api/v1/admin/users/")).status()).toBe(403);
  expect((await page.request.get("/api/v1/auth/sessions")).status()).toBe(403);
  await page.goto("/users"); await expect(page).toHaveURL(/\/account$/);
  await page.reload(); await expect(page.getByText("Wymagana zmiana hasła", { exact: true })).toBeVisible();
  await page.getByLabel("Dotychczasowe hasło", { exact: true }).fill("wrong-password");
  await page.getByLabel("Nowe hasło", { exact: true }).fill(next);
  await page.getByLabel("Potwierdź nowe hasło", { exact: true }).fill(next);
  await page.getByRole("button", { name: "Zmień hasło", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "Dotychczasowe hasło jest nieprawidłowe" })).toBeVisible();
  await expect(page.getByLabel("Nowe hasło", { exact: true })).toHaveValue(next);
  const oldCookies = await page.context().cookies();
  await changeTemporaryPassword(page, temporary, next);
  const newCookies = await page.context().cookies();
  for (const name of ["opensnow_session", "opensnow_csrf"]) expect(newCookies.find(cookie => cookie.name === name)!.value).not.toBe(oldCookies.find(cookie => cookie.name === name)!.value);
  await expect(page.getByText("Bieżąca sesja", { exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  const context = await browser.newContext({ baseURL: process.env.E2E_BASE_URL ?? "http://127.0.0.1:5173" });
  const other = await context.newPage();
  try {
    await signIn(other, email, next); await other.goto("/account");
    await expect(other.getByRole("heading", { name: "Moje konto", exact: true })).toBeVisible();
    await page.getByLabel("Nazwa wyświetlana", { exact: true }).fill("Lokalna propozycja");
    const me = await (await other.request.get("/api/v1/auth/me")).json();
    expect((await other.request.put("/api/v1/auth/profile", { headers: await csrf(other), data: { display_name: "Aktualna nazwa", version: me.user.version } })).status()).toBe(204);
    await page.getByRole("button", { name: "Zapisz profil", exact: true }).click();
    const conflict = page.getByRole("region", { name: "Konflikt wersji" });
    await expect(conflict).toContainText("Aktualna nazwa"); await expect(conflict).toContainText("Lokalna propozycja");
    await expect(page.getByLabel("Nazwa wyświetlana", { exact: true })).toHaveValue("Lokalna propozycja");
    await conflict.getByRole("button", { name: "Zachowaj propozycję do ponownego zapisu" }).click();
    await page.getByRole("button", { name: "Zapisz profil", exact: true }).click();
    await expect(page.getByRole("status").filter({ hasText: "Nazwa wyświetlana została zapisana" })).toBeVisible();
    await page.reload(); await expect(page.getByLabel("Nazwa wyświetlana", { exact: true })).toHaveValue("Lokalna propozycja");
    await page.getByRole("button", { name: "Odśwież sesje", exact: true }).click();
    await expect(page.getByRole("button", { name: "Unieważnij", exact: true })).toHaveCount(1);
    await page.getByRole("button", { name: "Unieważnij", exact: true }).focus();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("status").filter({ hasText: "Sesja została unieważniona" })).toBeVisible();
    expect((await other.request.get("/api/v1/auth/me")).status()).toBe(401);
    await other.reload(); await expect(other).toHaveURL(/\/login$/);
    const fresh = await other.request.post("/api/v1/auth/login", { data: { tenant, email, password: next } }); expect(fresh.status()).toBe(200);
    await page.getByRole("button", { name: "Odśwież sesje", exact: true }).click();
    await expect(page.getByRole("button", { name: "Unieważnij", exact: true })).toHaveCount(1);
    await page.getByRole("button", { name: "Unieważnij pozostałe sesje", exact: true }).click();
    await expect(page.getByRole("status").filter({ hasText: "Pozostałe sesje zostały unieważnione" })).toBeVisible();
    expect((await other.request.get("/api/v1/auth/me")).status()).toBe(401);
    expect((await other.request.post("/api/v1/auth/login", { data: { tenant, email, password: temporary } })).status()).toBe(401);
    await page.getByLabel("Nazwa wyświetlana", { exact: true }).focus(); await page.keyboard.press("Tab"); await expect(page.getByRole("button", { name: "Zapisz profil", exact: true })).toBeFocused();
    const path = info.outputPath("own-account.png"); await page.screenshot({ path, fullPage: true }); await info.attach("own-account", { path, contentType: "image/png" });
  } finally { await context.close(); }
});
