import { expect, test, type Page } from "@playwright/test";
import { changeTemporaryPassword, permanentRootPassword } from "./account-fixtures";

const tenant = process.env.E2E_TENANT ?? "e2e";
const email = process.env.E2E_ADMIN_EMAIL ?? "admin";
const password = process.env.E2E_ADMIN_PASSWORD;
if (!password) throw new Error("Missing E2E_ADMIN_PASSWORD");

async function fillInstaller(page: Page, login = email) {
  await page.getByLabel("Nazwa organizacji", { exact: true }).fill("E2E Tenant");
  await page.getByLabel("Identyfikator organizacji (tenant)").fill(tenant);
  await page.getByLabel("Nazwa administratora").fill("E2E Administrator");
  await page.getByLabel("Login lub e-mail").fill(login);
  await page.getByLabel("Hasło", { exact: true }).fill(password!);
  await page.getByLabel("Powtórz hasło").fill(password!);
}

async function submitInstaller(page: Page) {
  const response = page.waitForResponse(result => result.url().endsWith("/api/v1/setup") && result.request().method() === "POST");
  await page.getByRole("button", { name: "Utwórz konto i zakończ instalację" }).click();
  return response;
}

test("first-run installer creates the administrator and permanently closes setup", async ({ page, browser }, testInfo) => {
  test.setTimeout(90_000);
  const initial = await page.request.get("/api/v1/setup");
  expect(initial.status()).toBe(200);
  const installation = await initial.json();
  if (!installation.required) {
    await page.goto("/setup");
    await expect(page.getByRole("heading", { name: "Zaloguj się" })).toBeVisible();
    const replay = await page.request.post("/api/v1/setup", { data: { tenant, tenant_name: "Overwrite", email, display_name: "Overwrite", password } });
    expect(replay.status()).toBe(409);
    expect((await replay.json()).error.code).toBe("SETUP_COMPLETED");
    return;
  }

  await page.goto("/login");
  await expect(page).toHaveURL(/\/setup$/);
  await expect(page.getByRole("heading", { name: "Utwórz konto administratora" })).toBeVisible();
  const oppositeContext = await browser.newContext({
    baseURL: process.env.E2E_BASE_URL ?? "http://127.0.0.1:5173",
    viewport: testInfo.project.name === "chromium" ? { width: 320, height: 740 } : { width: 1280, height: 900 }
  });
  const stale = await oppositeContext.newPage();
  try {
    await stale.goto("/setup");
    await expect(stale.getByRole("heading", { name: "Utwórz konto administratora" })).toBeVisible();
    for (const [screen, name] of [[page, "installer-primary"], [stale, "installer-opposite-viewport"]] as const) {
      expect(await screen.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
      const path = testInfo.outputPath(`${name}.png`);
      await screen.screenshot({ path, fullPage: true });
      await testInfo.attach(name, { path, contentType: "image/png" });
    }
    await page.reload();
    await expect(page.getByRole("heading", { name: "Utwórz konto administratora" })).toBeVisible();
    await fillInstaller(page);
    await fillInstaller(stale);

    const status = await page.request.get("/api/v1/setup");
    const token = (await status.json()).token;
    expect((await page.request.post("/api/v1/setup", { data: {} })).status()).toBe(403);
    expect((await page.request.post("/api/v1/setup", { headers: { "X-Setup-Token": token, Origin: "https://example.invalid" }, data: {} })).status()).toBe(403);
    const badPassword = await page.request.post("/api/v1/setup", {
      headers: { "X-Setup-Token": token },
      data: { tenant, tenant_name: "E2E", email: "custom@example.test", display_name: "Custom", password: "admin" }
    });
    expect(badPassword.status()).toBe(422);
    await expect((await page.request.get("/api/v1/setup")).json().then(value => value.required)).resolves.toBe(true);

    await page.getByLabel("Powtórz hasło").fill("mismatch");
    await page.getByRole("button", { name: "Utwórz konto i zakończ instalację" }).click();
    await expect(page.getByRole("alert")).toHaveText("Hasła muszą być takie same.");
    await page.getByLabel("Powtórz hasło").fill(password!);
    await page.getByLabel("Login lub e-mail").fill("invalid-login");
    // Use a valid custom password so the server checks the login itself.
    await page.getByLabel("Hasło", { exact: true }).fill("installer-validation-password");
    await page.getByLabel("Powtórz hasło").fill("installer-validation-password");
    expect((await submitInstaller(page)).status()).toBe(422);
    await expect(page.getByRole("alert")).toContainText("valid email address");
    await fillInstaller(page);

    await page.context().clearCookies();
    expect((await submitInstaller(page)).status()).toBe(403);
    await expect(page.getByRole("alert")).toContainText("Sesja instalatora wygasła");
    await expect(page.getByLabel("Nazwa organizacji", { exact: true })).toHaveValue("E2E Tenant");
    // A transport failure leaves the form retryable without creating data.
    await page.route("**/api/v1/setup", route => route.request().method() === "POST" ? route.abort("failed") : route.continue());
    await page.getByRole("button", { name: "Utwórz konto i zakończ instalację" }).click();
    await expect(page.getByRole("alert")).toBeVisible();
    await expect(page.getByRole("button", { name: "Utwórz konto i zakończ instalację" })).toBeEnabled();
    await page.unroute("**/api/v1/setup");

    const created = await submitInstaller(page);
    expect(created.status()).toBe(201);
    expect(await created.json()).toEqual({ tenant, email: email === "admin" ? "admin@example.test" : email.toLowerCase() });
    await expect(page.getByRole("heading", { name: "Instalacja zakończona" })).toBeVisible();
    await expect((await page.request.get("/api/v1/setup")).json().then(value => value.required)).resolves.toBe(false);
    expect((await submitInstaller(stale)).status()).toBe(409);
    await expect(stale.getByRole("heading", { name: "Instalacja zakończona" })).toBeVisible();
    await page.getByRole("button", { name: "Przejdź do logowania" }).click();
    await expect(page.getByLabel("Organizacja", { exact: true })).toHaveValue(tenant);
    await expect(page.getByLabel("Login lub e-mail")).toHaveValue(email);
    await page.getByLabel("Hasło").fill(password!);
    await page.getByRole("button", { name: "Zaloguj" }).click();
    if (password === "admin") {
      await expect(page.getByRole("heading", { name: "Moje konto", exact: true })).toBeVisible();
      expect((await page.request.get("/api/v1/admin/users/")).status()).toBe(403);
      await changeTemporaryPassword(page, password, permanentRootPassword);
      await page.goto("/");
    }
    await expect(page.getByRole("heading", { name: "Mój obszar pracy", exact: true })).toBeVisible();
    const me = await page.request.get("/api/v1/auth/me");
    expect(me.status()).toBe(200);
    expect((await me.json()).permissions).toContain("admin.users.manage");
    await page.reload();
    await expect(page.getByRole("heading", { name: "Mój obszar pracy", exact: true })).toBeVisible();
    await page.getByRole("navigation", { name: "Główna nawigacja" }).getByRole("link", { name: "Audyt" }).click();
    await page.locator(".audit-entry").filter({ hasText: "setup.platform_admin" }).getByText("Szczegóły techniczne").click();
    await expect(page.getByText("setup.platform_admin", { exact: true })).toBeVisible();
    await page.goto("/setup");
    await expect(page.getByRole("heading", { name: "Mój obszar pracy", exact: true })).toBeVisible();
  } finally {
    await oppositeContext.close();
  }
});
