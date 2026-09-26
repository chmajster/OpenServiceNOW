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

function runScope(projectName: string, retry: number): string {
  const parts: string[] = [];
  if (projectName !== "chromium") {
    parts.push(projectName.replace(/[^a-z0-9]+/gi, "-").toLowerCase());
  }
  if (retry > 0) {
    parts.push(`retry${retry}`);
  }
  return parts.join("-");
}

function scopedEmail(base: string, scope: string): string {
  if (!scope) {
    return base;
  }
  const separator = base.lastIndexOf("@");
  if (separator <= 0) {
    throw new Error(`Invalid E2E operator email: ${base}`);
  }
  return `${base.slice(0, separator)}+${scope}${base.slice(separator)}`;
}

async function login(page: Page, email: string, password: string) {
  await page.goto("/login");
  await page.getByLabel("Tenant").fill(tenant);
  await page.getByLabel("E-mail").fill(email);
  await page.getByLabel("Hasło").fill(password);
  await page.getByRole("button", { name: "Zaloguj" }).click();
  await expect(page.getByRole("heading", { name: "Fundament platformy" })).toBeVisible();
}

test("admin creates a least-privilege tenant reader through the UI", async ({ page }, testInfo) => {
  const scope = runScope(testInfo.project.name, testInfo.retry);
  const operatorEmail = scopedEmail(operatorEmailBase, scope);
  const roleName = scope ? `${roleNameBase}_${scope.replaceAll("-", "_")}` : roleNameBase;
  const groupName = scope ? `e2e_security_group_${scope.replaceAll("-", "_")}` : "e2e_security_group";

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
  await expect(
    assignmentPanel.locator(".assignment-list").getByText(roleName, { exact: true })
  ).toBeVisible();

  const createGroupPanel = page.locator(".panel").filter({
    has: page.getByRole("heading", { name: "Nowa grupa" })
  });
  await createGroupPanel.getByLabel("Nazwa").fill(groupName);
  await createGroupPanel.getByLabel("Opis").fill("Security group created by Playwright E2E");
  await createGroupPanel.getByRole("button", { name: "Utwórz grupę" }).click();

  const createdGroupCard = page.locator(".list-card").filter({ hasText: groupName });
  await expect(createdGroupCard).toHaveCount(1);

  const groupAssignments = page.locator(".panel").filter({
    has: page.getByRole("heading", { name: "Przypisania grupowe" })
  });
  await groupAssignments.getByLabel("Grupa").selectOption({ label: groupName });

  const memberSection = groupAssignments.locator(".assignment-section").filter({
    has: page.getByRole("heading", { name: "Członkowie grupy" })
  });
  await memberSection.getByLabel("Użytkownik").selectOption({ label: `E2E Operator — ${operatorEmail}` });
  await memberSection.getByRole("button", { name: "Dodaj do grupy" }).click();
  await expect(memberSection.getByText(operatorEmail, { exact: true })).toBeVisible();

  const groupRoleSection = groupAssignments.locator(".assignment-section").filter({
    has: page.getByRole("heading", { name: "Role grupy" })
  });
  await groupRoleSection.getByLabel("Rola").selectOption({ label: roleName });
  await groupRoleSection.getByRole("button", { name: "Przypisz rolę grupie" }).click();
  await expect(
    groupRoleSection.locator(".assignment-list").getByText(roleName, { exact: true })
  ).toBeVisible();

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

  await page.getByRole("button", { name: "Wyloguj" }).first().click();
  await expect(page.getByRole("heading", { name: "Zaloguj się" })).toBeVisible();
  await login(page, adminEmail, adminPassword);

  await page.getByRole("link", { name: "Audyt" }).click();
  await expect(page.getByRole("heading", { name: "Audyt" })).toBeVisible();
  await expect(page.getByText("rbac.user.role.add", { exact: true })).toBeVisible();
  await expect(page.getByText("rbac.group.member.add", { exact: true })).toBeVisible();
  await expect(page.getByText("rbac.group.role.add", { exact: true })).toBeVisible();
});


test("tenant optimistic-lock conflict refreshes current data", async ({ browser }, testInfo) => {
  const firstContext = await browser.newContext();
  const secondContext = await browser.newContext();
  const firstPage = await firstContext.newPage();
  const secondPage = await secondContext.newPage();

  try {
    await login(firstPage, adminEmail, adminPassword);
    await login(secondPage, adminEmail, adminPassword);

    await firstPage.getByRole("link", { name: "Organizacja" }).click();
    await secondPage.getByRole("link", { name: "Organizacja" }).click();
    await expect(firstPage.getByRole("heading", { name: "Organizacja" })).toBeVisible();
    await expect(secondPage.getByRole("heading", { name: "Organizacja" })).toBeVisible();

    const firstName = firstPage.getByLabel("Nazwa organizacji");
    const secondName = secondPage.getByLabel("Nazwa organizacji");
    const originalName = await firstName.inputValue();
    const scope = runScope(testInfo.project.name, testInfo.retry);
    const concurrentName = scope ? `E2E Concurrent Tenant ${scope}` : "E2E Concurrent Tenant";

    await firstName.fill(concurrentName);
    await firstPage.getByRole("button", { name: "Zapisz zmianę" }).click();
    await expect(firstPage.getByText("Nazwa organizacji została zaktualizowana.")).toBeVisible();

    await secondName.fill(`${concurrentName} stale overwrite`);
    await secondPage.getByRole("button", { name: "Zapisz zmianę" }).click();
    await expect(
      secondPage.getByText("Rekord został zmieniony równolegle. Bieżące dane zostały odświeżone; sprawdź je i ponów zmianę.")
    ).toBeVisible();
    await expect(secondName).toHaveValue(concurrentName);

    await secondName.fill(originalName);
    await secondPage.getByRole("button", { name: "Zapisz zmianę" }).click();
    await expect(secondPage.getByText("Nazwa organizacji została zaktualizowana.")).toBeVisible();
    await expect(secondName).toHaveValue(originalName);
  } finally {
    await firstContext.close();
    await secondContext.close();
  }
});
