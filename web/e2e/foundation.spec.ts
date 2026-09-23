import { expect, test, type Page } from "@playwright/test";

const tenant = process.env.E2E_TENANT ?? "e2e";
const adminEmail = process.env.E2E_ADMIN_EMAIL ?? "admin@example.test";
const adminPassword = requireEnv("E2E_ADMIN_PASSWORD");
const operatorEmailBase = process.env.E2E_OPERATOR_EMAIL ?? "e2e-operator@example.test";
const operatorPassword = requireEnv("E2E_OPERATOR_PASSWORD");
const roleNameBase = "e2e_tenant_reader";

function requireEnv(name: string): string {
  const value = process.env[name];
  if (!value) {
    throw new Error(`Missing required environment variable: ${name}`);
  }
  return value;
}

function retryScopedEmail(base: string, retry: number): string {
  if (retry === 0) {
    return base;
  }
  const separator = base.lastIndexOf("@");
  if (separator <= 0) {
    throw new Error(`Invalid E2E operator email: ${base}`);
  }
  return `${base.slice(0, separator)}+retry${retry}${base.slice(separator)}`;
}

async function login(page: Page, email: string, password: string) {
  await page.goto("/login");
  await page.getByLabel("Tenant").fill(tenant);
  await page.getByLabel("E-mail").fill(email);
  await page.getByLabel("Hasło").fill(password);
  await page.getByRole("button", { name: "Zaloguj" }).click();
  await expect(page.getByText("Sesja aktywna")).toBeVisible();
}

test("admin creates a least-privilege tenant reader through the UI", async ({ page }, testInfo) => {
  const operatorEmail = retryScopedEmail(operatorEmailBase, testInfo.retry);
  const roleName = testInfo.retry === 0 ? roleNameBase : `${roleNameBase}_retry${testInfo.retry}`;

  await login(page, adminEmail, adminPassword);

  await page.getByRole("link", { name: "Użytkownicy" }).click();
  await expect(page.getByRole("heading", { name: "Użytkownicy" })).toBeVisible();

  const createUserPanel = page.locator(".panel").filter({
    has: page.getByRole("heading", { name: "Dodaj konto lokalne" })
  });
  await createUserPanel.getByLabel("E-mail").fill(operatorEmail);
  await createUserPanel.getByLabel("Nazwa wyświetlana").fill("E2E Operator");
  await createUserPanel.getByLabel("Hasło").fill(operatorPassword);
  await createUserPanel.getByRole("button", { name: "Dodaj użytkownika" }).click();
  await expect(page.getByText(operatorEmail)).toBeVisible();

  await page.getByRole("link", { name: "Role i RBAC" }).click();
  await expect(page.getByRole("heading", { name: "Role i RBAC" })).toBeVisible();

  const createRolePanel = page.locator(".panel").filter({
    has: page.getByRole("heading", { name: "Nowa rola" })
  });
  await createRolePanel.getByLabel("Nazwa").fill(roleName);
  await createRolePanel.getByLabel("Opis").fill("Read-only tenant access created by Playwright E2E");

  await createRolePanel
    .locator("label.checkbox-row")
    .filter({ hasText: "tenant.read" })
    .getByRole("checkbox")
    .check();

  await createRolePanel.getByRole("button", { name: "Utwórz rolę" }).click();
  const createdRoleCard = page.locator(".list-card").filter({ hasText: roleName });
  await expect(createdRoleCard).toHaveCount(1);
  await expect(createdRoleCard.getByText(roleName, { exact: true })).toBeVisible();

  const assignmentPanel = page.locator(".panel").filter({
    has: page.getByRole("heading", { name: "Przypisz rolę użytkownikowi" })
  });

  await assignmentPanel.getByLabel("Wyszukaj użytkownika").fill(operatorEmail);
  await assignmentPanel.getByRole("button", { name: "Szukaj" }).click();

  const userSelect = assignmentPanel.getByLabel("Użytkownik");
  await expect(userSelect.locator("option", { hasText: operatorEmail })).toHaveCount(1);
  await userSelect.selectOption({ label: `E2E Operator — ${operatorEmail}` });

  await assignmentPanel.getByLabel("Rola do przypisania").selectOption({ label: roleName });
  await assignmentPanel.getByRole("button", { name: "Przypisz rolę" }).click();
  await expect(page.getByText("Rola została przypisana użytkownikowi.")).toBeVisible();

  await page.getByRole("button", { name: "Wyloguj" }).first().click();
  await expect(page.getByRole("heading", { name: "Zaloguj się" })).toBeVisible();

  await login(page, operatorEmail, operatorPassword);

  await expect(page.getByRole("link", { name: "Organizacja" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Użytkownicy" })).toHaveCount(0);
  await expect(page.getByRole("link", { name: "Role i RBAC" })).toHaveCount(0);
  await expect(page.getByRole("link", { name: "Audyt" })).toHaveCount(0);

  const forbiddenUsers = await page.request.get("/api/v1/admin/users/");
  expect(forbiddenUsers.status()).toBe(403);

  await page.goto("/users");
  await expect(page.getByText("Brak uprawnień")).toBeVisible();

  await page.getByRole("link", { name: "Organizacja" }).click();
  await expect(page.getByRole("heading", { name: "Organizacja" })).toBeVisible();
  await expect(page.getByText("Tryb tylko do odczytu")).toBeVisible();
  await expect(page.getByLabel("Nazwa organizacji")).toBeDisabled();
});
