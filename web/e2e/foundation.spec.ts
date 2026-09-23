import { expect, test, type Page } from "@playwright/test";

const tenant = process.env.E2E_TENANT ?? "e2e";
const adminEmail = process.env.E2E_ADMIN_EMAIL ?? "admin@example.test";
const adminPassword = requireEnv("E2E_ADMIN_PASSWORD");
const operatorEmail = process.env.E2E_OPERATOR_EMAIL ?? "e2e-operator@example.test";
const operatorPassword = requireEnv("E2E_OPERATOR_PASSWORD");
const roleName = "e2e_tenant_reader";

function requireEnv(name: string): string {
  const value = process.env[name];
  if (!value) {
    throw new Error(`Missing required environment variable: ${name}`);
  }
  return value;
}

async function login(page: Page, email: string, password: string) {
  await page.goto("/login");
  await page.getByLabel("Tenant").fill(tenant);
  await page.getByLabel("E-mail").fill(email);
  await page.getByLabel("Hasło").fill(password);
  await page.getByRole("button", { name: "Zaloguj" }).click();
  await expect(page.getByText("Sesja aktywna")).toBeVisible();
}

test("admin creates a least-privilege tenant reader through the UI", async ({ page }) => {
  await login(page, adminEmail, adminPassword);

  await page.getByRole("link", { name: "Użytkownicy" }).click();
  await expect(page.getByRole("heading", { name: "Użytkownicy" })).toBeVisible();

  const createUserPanel = page.getByRole("heading", { name: "Dodaj konto lokalne" }).locator("..").locator("..");
  await createUserPanel.getByLabel("E-mail").fill(operatorEmail);
  await createUserPanel.getByLabel("Nazwa wyświetlana").fill("E2E Operator");
  await createUserPanel.getByLabel("Hasło").fill(operatorPassword);
  await createUserPanel.getByRole("button", { name: "Dodaj użytkownika" }).click();
  await expect(page.getByText(operatorEmail)).toBeVisible();

  await page.getByRole("link", { name: "Role i RBAC" }).click();
  await expect(page.getByRole("heading", { name: "Role i RBAC" })).toBeVisible();

  const createRolePanel = page.getByRole("heading", { name: "Nowa rola" }).locator("..").locator("..");
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

  const assignmentPanel = page
    .getByRole("heading", { name: "Przypisz rolę użytkownikowi" })
    .locator("..")
    .locator("..");

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

  await page.getByRole("link", { name: "Organizacja" }).click();
  await expect(page.getByRole("heading", { name: "Organizacja" })).toBeVisible();
  await expect(page.getByText("Tryb tylko do odczytu")).toBeVisible();
  await expect(page.getByLabel("Nazwa organizacji")).toBeDisabled();
});
