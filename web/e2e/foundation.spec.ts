import { expect, test, type Page, type Locator } from "@playwright/test";
import { changeTemporaryPassword, permanentRootPassword, personalPassword } from "./account-fixtures";

const tenant = process.env.E2E_TENANT ?? "e2e";
let adminEmail = process.env.E2E_ADMIN_EMAIL ?? "admin@example.test";
let adminPassword = requireEnv("E2E_ADMIN_PASSWORD");
const operatorPassword = requireEnv("E2E_OPERATOR_PASSWORD");
function requireEnv(name: string) {
  const value = process.env[name];
  if (!value) throw new Error(`Missing required environment variable: ${name}`);
  return value;
}
function scope() {
  return `${test.info().project.name}-${test.info().retry}`.replace(
    /[^a-z0-9-]/gi,
    "",
  );
}

// Independent real administrator accounts keep each browser project's login
// workload within the production account limiter, without weakening its policy.
test.beforeAll(async ({ request }, testInfo) => {
  const email = `suite-admin-${testInfo.project.name}@example.test`;
  const result = await request.post("/api/v1/auth/login", {
    data: {
      tenant,
      email: process.env.E2E_ADMIN_EMAIL ?? "admin",
      password: permanentRootPassword,
    },
  });
  expect(result.status()).toBe(200);
  const token = (await request.storageState()).cookies.find(
    (cookie) => cookie.name === "opensnow_csrf",
  )!.value;
  const headers = { "X-CSRF-Token": token };
  const created = await request.post("/api/v1/admin/users/", {
    headers,
    data: {
      email,
      display_name: "Administrator testów",
      password: operatorPassword,
    },
  });
  expect([201, 409]).toContain(created.status());
  const users = await (
    await request.get(`/api/v1/admin/users/?q=${email}`)
  ).json();
  const roles = await (
    await request.get("/api/v1/admin/rbac/roles?q=platform_admin")
  ).json();
  expect(
    (
      await request.post(
        `/api/v1/admin/rbac/users/${users.users[0].id}/roles/${roles.roles[0].id}`,
        { headers },
      )
    ).status(),
  ).toBe(204);
  await request.post("/api/v1/auth/logout", { headers });
  adminEmail = email;
  const signedIn = await request.post("/api/v1/auth/login", { data: { tenant, email, password: operatorPassword } });
  expect(signedIn.status()).toBe(200);
  const signedInData = await signedIn.json();
  const ownCSRF = (await request.storageState()).cookies.find(cookie => cookie.name === "opensnow_csrf")!.value;
  expect((await request.post("/api/v1/auth/password", { headers: { "X-CSRF-Token": ownCSRF }, data: { current_password: operatorPassword, new_password: personalPassword(operatorPassword), credential_version: signedInData.user.credential_version } })).status()).toBe(200);
  adminPassword = personalPassword(operatorPassword);
});

function contextOptions() {
  return {
    baseURL: process.env.E2E_BASE_URL ?? "http://127.0.0.1:5173",
    ...(test.info().project.name === "mobile-chromium"
      ? {
          viewport: { width: 412, height: 915 },
          isMobile: true,
          hasTouch: true,
        }
      : {}),
  };
}
async function submitLogin(page: Page) {
  const submit = async () => {
    const response = page.waitForResponse(
      (result) =>
        result.url().endsWith("/api/v1/auth/login") &&
        result.request().method() === "POST",
    );
    await page.getByRole("button", { name: "Zaloguj" }).click();
    return response;
  };
  let response = await submit();
  if (response.status() === 429) {
    const retryAfter = Number(response.headers()["retry-after"]);
    expect(retryAfter).toBeGreaterThan(0);
    expect(retryAfter).toBeLessThanOrEqual(60);
    test.setTimeout(test.info().timeout + retryAfter * 1000 + 5000);
    await new Promise((resolve) => setTimeout(resolve, retryAfter * 1000));
    response = await submit();
  }
  return response;
}
async function login(page: Page, email = adminEmail, password = adminPassword) {
  await page.goto("/login");
  await page.getByLabel("Organizacja", { exact: true }).fill(tenant);
  await page.getByLabel("Login lub e-mail").fill(email);
  await page.getByLabel("Hasło").fill(password);
  expect((await submitLogin(page)).status()).toBe(200);
  const me = await (await page.request.get("/api/v1/auth/me")).json();
  if (me.password_change_required) {
    await changeTemporaryPassword(page, password, personalPassword(password));
    await page.goto("/");
  }
  await expect(
    page.getByRole("heading", { name: "Przegląd", exact: true }),
  ).toBeVisible();
}
async function csrf(page: Page) {
  return {
    "X-CSRF-Token": (await page.context().cookies()).find(
      (cookie) => cookie.name === "opensnow_csrf",
    )!.value,
  };
}
async function createRole(
  page: Page,
  name: string,
  permissions = ["tenant.read"],
) {
  const response = await page.request.post("/api/v1/admin/rbac/roles", {
    headers: await csrf(page),
    data: { name, permissions },
  });
  expect(response.status()).toBe(201);
  return (await response.json()).id as string;
}
async function select(
  container: Page | Locator,
  label: string,
  query: string,
  option: string,
) {
  const input = container.getByRole("combobox", { name: label, exact: true });
  await input.fill(query);
  await expect(
    container.getByRole("option", { name: option, exact: true }),
  ).toBeVisible();
  await input.press("ArrowDown");
  await input.press("End");
  await input.press("Home");
  await input.press("Enter");
  await expect(input).toHaveValue(option);
}
function checkbox(container: Page | Locator, key: string) {
  return container
    .locator("label.checkbox-row")
    .filter({ hasText: key })
    .getByRole("checkbox");
}
function detail(page: Page, kind: string, id: string, tab: string) {
  return page.goto(`/rbac?kind=${kind}&id=${id}&tab=${tab}`);
}

test("login rejects invalid credentials, restores a session and confirms logout", async ({
  page,
}) => {
  await page.goto("/login");
  await page.getByLabel("Organizacja", { exact: true }).fill(tenant);
  await page.getByLabel("Login lub e-mail").fill(adminEmail);
  await page.getByLabel("Hasło").fill("wrong");
  expect((await submitLogin(page)).status()).toBe(401);
  await expect(page.getByRole("alert")).toHaveText(
    "Nieprawidłowy tenant, login lub hasło.",
  );
  expect((await page.request.get("/api/v1/admin/users/")).status()).toBe(401);
  await login(page);
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "Przegląd", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Wyloguj" }).first().click();
  await expect(
    page.getByRole("heading", { name: "Zaloguj się" }),
  ).toBeVisible();
  expect((await page.request.get("/api/v1/auth/me")).status()).toBe(401);
  await expect(page.getByText("Zalogowano", { exact: true })).toHaveCount(0);
});

test("admin manages a least-privilege user, role, group and audit with the keyboard", async ({
  page,
}, testInfo) => {
  const suffix = scope();
  const email = `operator-${suffix}@example.test`;
  const roleName = `reader-${suffix}`;
  const groupName = `security-${suffix}`;
  await login(page);
  await page.goto("/users");
  await page.getByRole("button", { name: "Nowy użytkownik" }).click();
  const form = page.locator(".panel").filter({
    has: page.getByRole("heading", { name: "Dodaj konto lokalne" }),
  });
  await form.getByLabel("E-mail", { exact: true }).fill(email);
  await form.getByLabel("Nazwa wyświetlana").fill("Operator E2E");
  await form.getByLabel("Hasło").fill(operatorPassword);
  await form.getByRole("button", { name: "Dodaj użytkownika" }).click();
  await expect(page.getByText(email, { exact: true })).toBeVisible();
  const users = await (
    await page.request.get(`/api/v1/admin/users/?q=${email}`)
  ).json();
  const userID = users.users[0].id as string;
  await page.goto("/rbac?kind=roles");
  await page.getByRole("button", { name: "Nowa rola" }).click();
  await page.getByLabel("Nazwa", { exact: true }).fill(roleName);
  await page
    .getByLabel("Opis", { exact: true })
    .fill("Dostęp tylko do odczytu");
  await checkbox(page, "tenant.read").check();
  await page.getByRole("button", { name: "Utwórz rolę" }).click();
  await expect(
    page.getByRole("heading", { name: roleName, exact: true }),
  ).toBeVisible();
  const roleID = new URL(page.url()).searchParams.get("id")!;
  await page.getByRole("link", { name: "Przypisania", exact: true }).click();
  const userSection = page
    .locator("section")
    .filter({
      has: page.getByRole("heading", { name: "Role użytkownika", exact: true }),
    })
    .last();
  await select(userSection, "Użytkownik", email, `Operator E2E — ${email}`);
  await userSection
    .getByRole("button", { name: "Przypisz rolę", exact: true })
    .click();
  await expect(
    userSection
      .getByRole("status")
      .filter({ hasText: "Rola została przypisana" }),
  ).toBeVisible();
  await expect(
    userSection.getByRole("button", { name: `Usuń rolę ${roleName}` }),
  ).toBeEnabled();
  await userSection
    .getByRole("button", { name: `Usuń rolę ${roleName}` })
    .click();
  await expect(
    userSection.getByText("Brak bezpośrednio przypisanych ról."),
  ).toBeVisible();
  await select(userSection, "Rola do przypisania", roleName, roleName);
  await userSection
    .getByRole("button", { name: "Przypisz rolę", exact: true })
    .click();
  await expect(
    userSection.getByRole("button", { name: `Usuń rolę ${roleName}` }),
  ).toBeEnabled();
  await page.goto("/rbac?kind=groups");
  await page.getByRole("button", { name: "Nowa grupa" }).click();
  await page.getByLabel("Nazwa", { exact: true }).fill(groupName);
  await page.getByRole("button", { name: "Utwórz grupę" }).click();
  await expect(
    page.getByRole("heading", { name: groupName, exact: true }),
  ).toBeVisible();
  const groupID = new URL(page.url()).searchParams.get("id")!;
  await page.getByRole("link", { name: "Członkowie", exact: true }).click();
  await select(page, "Użytkownik do dodania", email, `Operator E2E — ${email}`);
  await page.getByRole("button", { name: "Dodaj do grupy" }).click();
  await expect(
    page.getByRole("button", { name: "Usuń członka Operator E2E" }),
  ).toBeEnabled();
  const memberImage = testInfo.outputPath("group-members.png");
  await page.screenshot({ path: memberImage, fullPage: true });
  await testInfo.attach("group-members", {
    path: memberImage,
    contentType: "image/png",
  });
  await page.getByRole("button", { name: "Usuń członka Operator E2E" }).click();
  await expect(page.getByText("Grupa nie ma członków.")).toBeVisible();
  await select(page, "Użytkownik do dodania", email, `Operator E2E — ${email}`);
  await page.getByRole("button", { name: "Dodaj do grupy" }).click();
  await expect(
    page.getByRole("button", { name: "Usuń członka Operator E2E" }),
  ).toBeEnabled();
  await page.getByRole("link", { name: "Przypisania", exact: true }).click();
  await select(page, "Rola do przypisania", roleName, roleName);
  await page
    .getByRole("button", { name: "Przypisz rolę", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: `Usuń rolę ${roleName}` }),
  ).toBeEnabled();
  await page.getByRole("button", { name: `Usuń rolę ${roleName}` }).click();
  await expect(
    page
      .getByText("Brak bezpośrednio przypisanych ról.")
      .filter({ visible: true }),
  ).toBeVisible();
  await select(page, "Rola do przypisania", roleName, roleName);
  await page
    .getByRole("button", { name: "Przypisz rolę", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: `Usuń rolę ${roleName}` }),
  ).toBeEnabled();
  await page.getByRole("button", { name: "Wyloguj" }).first().click();
  await login(page, email, operatorPassword);
  await expect(
    page
      .locator(".navigation")
      .getByRole("link", { name: "Organizacja", exact: true }),
  ).toBeVisible();
  for (const name of ["Użytkownicy", "Role", "Grupy", "Audyt", "Diagnostyka"])
    await expect(
      page.locator(".navigation").getByRole("link", { name, exact: true }),
    ).toHaveCount(0);
  expect((await page.request.get("/api/v1/admin/users/")).status()).toBe(403);
  await page.goto("/users");
  await expect(page.getByText("Brak uprawnień", { exact: true })).toBeVisible();
  await page.goto("/tenant");
  await expect(page.getByText("Tryb tylko do odczytu")).toBeVisible();
  await expect(page.getByLabel("Nazwa organizacji")).toBeDisabled();
  await page.getByRole("button", { name: "Wyloguj" }).first().click();
  await login(page);
  await page.goto(
    `/audit?action=rbac.group.member.add&object_type=group&object_id=${groupID}&actor_user_id=${(await (await page.request.get("/api/v1/auth/me")).json()).user.id}`,
  );
  await expect(page.locator(".audit-entry")).toHaveCount(2);
  await page.reload();
  await expect(
    page.getByLabel("Identyfikator obiektu", { exact: true }),
  ).toHaveValue(groupID);
  const events = await (
    await page.request.get(
      `/api/v1/admin/audit/?action=rbac.group.member.add&object_id=${groupID}`,
    )
  ).json();
  const requestID = events.events[0].request_id;
  await page.getByLabel("Identyfikator żądania (request_id)").fill(requestID);
  await page.getByRole("button", { name: "Zastosuj filtry" }).click();
  await expect(page.locator(".audit-entry")).toHaveCount(1);
  await page.locator(".audit-entry").getByText("Szczegóły techniczne").click();
  await expect(page.locator(".audit-entry")).toContainText(requestID);
  expect(
    (
      await page.request.get(`/api/v1/admin/rbac/users/${userID}/roles`)
    ).status(),
  ).toBe(200);
  expect(
    (await page.request.get(`/api/v1/admin/rbac/roles/${roleID}`)).status(),
  ).toBe(200);
  await expect(
    page.getByRole("navigation", { name: "Ścieżka nawigacji" }),
  ).toBeVisible();
  const auditImage = testInfo.outputPath("audit-details.png");
  await page.screenshot({ path: auditImage, fullPage: true });
  await testInfo.attach("audit-details", {
    path: auditImage,
    contentType: "image/png",
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});

test("tenant conflict preserves local work and requires an explicit decision", async ({
  browser,
}) => {
  const first = await browser.newContext(contextOptions());
  const second = await browser.newContext(contextOptions());
  const a = await first.newPage();
  const b = await second.newPage();
  try {
    await login(a);
    await login(b);
    await a.goto("/tenant");
    await b.goto("/tenant");
    const original = await a.getByLabel("Nazwa organizacji").inputValue();
    const remote = `Organizacja równoległa ${scope()}`;
    const local = `Moja propozycja ${scope()}`;
    await a.getByLabel("Nazwa organizacji").fill(remote);
    await a.getByRole("button", { name: "Zapisz zmianę" }).click();
    await expect(
      a.getByText("Nazwa organizacji została zaktualizowana."),
    ).toBeVisible();
    await expect(a.locator(".topbar")).toContainText(remote);
    await b.getByLabel("Nazwa organizacji").fill(local);
    await b.getByRole("button", { name: "Zapisz zmianę" }).click();
    const conflict = b.getByRole("region", { name: "Konflikt wersji" });
    await expect(conflict).toContainText(remote);
    await expect(conflict).toContainText(local);
    await expect(b.getByLabel("Nazwa organizacji")).toHaveValue(local);
    await expect(
      b.getByRole("button", { name: "Zapisz zmianę" }),
    ).toBeDisabled();
    await b
      .getByRole("button", { name: "Zachowaj propozycję do ponownego zapisu" })
      .click();
    await b.getByRole("button", { name: "Zapisz zmianę" }).click();
    await expect(
      b.getByText("Nazwa organizacji została zaktualizowana."),
    ).toBeVisible();
    await expect(b.locator(".topbar")).toContainText(local);
    await b.getByLabel("Nazwa organizacji").fill(original);
    await b.getByRole("button", { name: "Zapisz zmianę" }).click();
    await expect(
      b.getByText("Nazwa organizacji została zaktualizowana."),
    ).toBeVisible();
  } finally {
    await first.close();
    await second.close();
  }
});

test("role conflict keeps checkboxes and shows the current permissions", async ({
  browser,
}) => {
  const first = await browser.newContext(contextOptions());
  const second = await browser.newContext(contextOptions());
  const a = await first.newPage();
  const b = await second.newPage();
  try {
    await login(a);
    await login(b);
    const id = await createRole(a, `conflict-${scope()}`);
    await detail(a, "roles", id, "permissions");
    await detail(b, "roles", id, "permissions");
    await expect(checkbox(a, "tenant.read")).toBeChecked();
    await expect(checkbox(b, "tenant.read")).toBeChecked();
    await checkbox(a, "tenant.manage").check();
    await a.getByRole("button", { name: "Zapisz uprawnienia" }).click();
    await expect(
      a.getByText("Uprawnienia roli zostały zaktualizowane."),
    ).toBeVisible();
    await checkbox(b, "admin.audit.read").check();
    await b.route(`**/api/v1/admin/rbac/roles/${id}`, (route) =>
      route.abort("failed"),
    );
    await b.getByRole("button", { name: "Zapisz uprawnienia" }).click();
    await expect(
      b.getByRole("button", { name: "Pobierz aktualny stan" }),
    ).toBeVisible();
    await expect(checkbox(b, "admin.audit.read")).toBeChecked();
    await expect(
      b.getByRole("button", { name: "Zapisz uprawnienia" }),
    ).toBeDisabled();
    await b.unroute(`**/api/v1/admin/rbac/roles/${id}`);
    await b.getByRole("button", { name: "Pobierz aktualny stan" }).click();
    const conflict = b.getByRole("region", { name: "Konflikt wersji" });
    await expect(conflict).toContainText("tenant.manage");
    await expect(conflict).toContainText("admin.audit.read");
    await expect(checkbox(b, "admin.audit.read")).toBeChecked();
    await expect(
      b.getByRole("button", { name: "Zapisz uprawnienia" }),
    ).toBeDisabled();
    await b
      .getByRole("button", { name: "Zachowaj propozycję do ponownego zapisu" })
      .click();
    await b.getByRole("button", { name: "Zapisz uprawnienia" }).click();
    await expect(
      b.getByText("Uprawnienia roli zostały zaktualizowane."),
    ).toBeVisible();
    const saved = await (
      await b.request.get(`/api/v1/admin/rbac/roles/${id}`)
    ).json();
    expect(
      saved.role.permissions.map(
        (permission: { key: string }) => permission.key,
      ),
    ).toEqual(["admin.audit.read", "tenant.read"]);
  } finally {
    await first.close();
    await second.close();
  }
});

test("remote user pagination reaches beyond the first 50 and late A cannot change B", async ({
  page,
}) => {
  test.setTimeout(150_000);
  await login(page);
  const headers = await csrf(page);
  const suffix = scope();
  let lastID = "";
  for (let i = 0; i < 55; i++) {
    const result = await page.request.post("/api/v1/admin/users/", {
      headers,
      data: {
        email: `pagination-${suffix}-${i}@example.test`,
        display_name: `Strona ${i}`,
        password: operatorPassword,
      },
    });
    expect(result.status()).toBe(201);
    lastID = (await result.json()).id;
  }
  const roleName = `pagination-role-${suffix}`;
  const roleID = await createRole(page, roleName);
  await detail(page, "roles", roleID, "assignments");
  const section = page
    .locator("section")
    .filter({
      has: page.getByRole("heading", { name: "Role użytkownika", exact: true }),
    })
    .last();
  const input = section.getByRole("combobox", {
    name: "Użytkownik",
    exact: true,
  });
  await input.click();
  await expect(section.getByRole("option")).toHaveCount(50);
  const option = `Strona 54 — pagination-${suffix}-54@example.test`;
  await expect(
    section.getByRole("option", { name: option, exact: true }),
  ).toHaveCount(0);
  for (
    let i = 0;
    i < 10 &&
    !(await section.getByRole("option", { name: option, exact: true }).count());
    i++
  ) {
    await section
      .getByRole("button", { name: "Załaduj więcej wyników" })
      .click();
    await expect(section.getByText("Wyszukiwanie…")).toHaveCount(0);
  }
  await section.getByRole("option", { name: option, exact: true }).click();
  await expect(input).toHaveValue(option);
  await section
    .getByRole("button", { name: "Przypisz rolę", exact: true })
    .click();
  await expect(
    section.getByRole("button", { name: `Usuń rolę ${roleName}` }),
  ).toBeEnabled();
  expect(
    (
      await (
        await page.request.get(`/api/v1/admin/rbac/users/${lastID}/roles`)
      ).json()
    ).roles.map((role: { id: string }) => role.id),
  ).toContain(roleID);
  // Delay an actual response from MySQL; the browser must never expose A's roles for B.
  await select(
    section,
    "Użytkownik",
    `pagination-${suffix}-53@`,
    `Strona 53 — pagination-${suffix}-53@example.test`,
  );
  await expect(
    section.getByText("Brak bezpośrednio przypisanych ról."),
  ).toBeVisible();
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  let requested!: () => void;
  const seen = new Promise<void>((resolve) => {
    requested = resolve;
  });
  await page.route(
    `**/api/v1/admin/rbac/users/${lastID}/roles?*`,
    async (route) => {
      const response = await route.fetch();
      requested();
      await gate;
      await route.fulfill({ response }).catch(() => undefined);
    },
  );
  await select(section, "Użytkownik", `pagination-${suffix}-54@`, option);
  await seen;
  await select(
    section,
    "Użytkownik",
    `pagination-${suffix}-53@`,
    `Strona 53 — pagination-${suffix}-53@example.test`,
  );
  await expect(
    section.getByText("Brak bezpośrednio przypisanych ról."),
  ).toBeVisible();
  release();
  await expect(
    section.getByRole("button", { name: `Usuń rolę ${roleName}` }),
  ).toHaveCount(0);
  const groups: { id: string; name: string }[] = [];
  for (const name of [`late-group-A-${suffix}`, `late-group-B-${suffix}`]) {
    const response = await page.request.post("/api/v1/admin/rbac/groups", {
      headers: await csrf(page),
      data: { name },
    });
    expect(response.status()).toBe(201);
    groups.push({ id: (await response.json()).id, name });
  }
  const groupA = groups[0]!;
  const groupB = groups[1]!;
  expect(
    (
      await page.request.post(
        `/api/v1/admin/rbac/groups/${groupA.id}/roles/${roleID}`,
        { headers: await csrf(page) },
      )
    ).status(),
  ).toBe(204);
  let releaseGroup!: () => void;
  const groupGate = new Promise<void>((resolve) => {
    releaseGroup = resolve;
  });
  let sawGroup!: () => void;
  const groupSeen = new Promise<void>((resolve) => {
    sawGroup = resolve;
  });
  await page.route(
    `**/api/v1/admin/rbac/groups/${groupA.id}/roles?*`,
    async (route) => {
      const response = await route.fetch();
      sawGroup();
      await groupGate;
      await route.fulfill({ response }).catch(() => undefined);
    },
  );
  const groupSection = page
    .locator("section")
    .filter({
      has: page.getByRole("heading", {
        name: "Role wybranej grupy",
        exact: true,
      }),
    })
    .last();
  await select(groupSection, "Grupa", groupA.name, groupA.name);
  await groupSeen;
  await expect(
    groupSection.getByRole("button", { name: "Przypisz rolę", exact: true }),
  ).toBeDisabled();
  await select(groupSection, "Grupa", groupB.name, groupB.name);
  await expect(
    groupSection.getByText("Brak bezpośrednio przypisanych ról."),
  ).toBeVisible();
  releaseGroup();
  await expect(
    groupSection.getByRole("button", { name: `Usuń rolę ${roleName}` }),
  ).toHaveCount(0);
  await page.reload();
  await expect(
    page.getByRole("link", { name: "Przypisania", exact: true }),
  ).toHaveAttribute("aria-current", "page");
});

test("catalog retry and duplicate creation preserve the role draft", async ({
  page,
}) => {
  await login(page);
  await page.goto("/rbac?kind=roles");
  await page.route("**/api/v1/admin/rbac/permissions", (route) => {
    return route.abort("failed");
  });
  await page.getByRole("button", { name: "Nowa rola" }).click();
  const name = `draft-${scope()}`;
  await page.getByLabel("Nazwa", { exact: true }).fill(name);
  const description = page.getByRole("textbox", { name: "Opis", exact: true });
  await description.fill("Zachowaj tę pracę");
  await expect(
    page.getByRole("button", { name: "Pobierz katalog uprawnień" }),
  ).toBeVisible();
  await page.unroute("**/api/v1/admin/rbac/permissions");
  await page.getByRole("button", { name: "Pobierz katalog uprawnień" }).click();
  await expect(page.getByLabel("Nazwa", { exact: true })).toHaveValue(name);
  await checkbox(page, "tenant.read").check();
  await createRole(page, name, []);
  await page.getByRole("button", { name: "Utwórz rolę" }).click();
  await expect(page.getByRole("alert")).toContainText("Ta nazwa już istnieje");
  await expect(checkbox(page, "tenant.read")).toBeChecked();
  await expect(description).toHaveValue("Zachowaj tę pracę");
  await page.getByLabel("Nazwa", { exact: true }).fill(`${name}-resolved`);
  await page.getByRole("button", { name: "Utwórz rolę" }).click();
  await expect(
    page.getByRole("heading", { name: `${name}-resolved`, exact: true }),
  ).toBeVisible();
  await expect(page.locator(".navigation a[aria-current='page']")).toHaveCount(
    1,
  );
});

test("backend logout invalidates every screen and a fresh login restores context", async ({
  page,
}) => {
  await login(page);
  await page.goto("/users?q=admin");
  await expect(
    page.getByRole("heading", { name: "Użytkownicy", exact: true }),
  ).toBeVisible();
  expect(
    (
      await page.request.post("/api/v1/auth/logout", {
        headers: await csrf(page),
      })
    ).status(),
  ).toBe(204);
  await page.getByRole("button", { name: "Odśwież", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Zaloguj się" }),
  ).toBeVisible();
  await expect(page.getByText("Zalogowano", { exact: true })).toHaveCount(0);
  await expect(page.locator(".sidebar")).toHaveCount(0);
  await login(page);
  await page.goto("/users?q=admin");
  await page.reload();
  await expect(page.getByLabel("Szukaj po nazwie lub e-mailu")).toHaveValue(
    "admin",
  );
});

test("editing own permissions refreshes navigation and backend access", async ({
  page,
}) => {
  await login(page);
  const email = `own-permissions-${scope()}@example.test`;
  const result = await page.request.post("/api/v1/admin/users/", {
    headers: await csrf(page),
    data: {
      email,
      display_name: "Edytor własnych uprawnień",
      password: operatorPassword,
    },
  });
  expect(result.status()).toBe(201);
  const userID = (await result.json()).id;
  const roleID = await createRole(page, `own-role-${scope()}`, [
    "tenant.read",
    "admin.rbac.manage",
    "admin.users.manage",
  ]);
  expect(
    (
      await page.request.post(
        `/api/v1/admin/rbac/users/${userID}/roles/${roleID}`,
        { headers: await csrf(page) },
      )
    ).status(),
  ).toBe(204);
  await page.getByRole("button", { name: "Wyloguj" }).first().click();
  await login(page, email, operatorPassword);
  await detail(page, "roles", roleID, "permissions");
  await checkbox(page, "admin.users.manage").uncheck();
  await page.getByRole("button", { name: "Zapisz uprawnienia" }).click();
  await expect(
    page.getByText("Uprawnienia roli zostały zaktualizowane."),
  ).toBeVisible();
  await expect(
    page
      .locator(".navigation")
      .getByRole("link", { name: "Użytkownicy", exact: true }),
  ).toHaveCount(0);
  expect((await page.request.get("/api/v1/admin/users/")).status()).toBe(403);
  await checkbox(page, "admin.users.manage").check();
  await page.getByRole("button", { name: "Zapisz uprawnienia" }).click();
  await expect(
    page
      .locator(".navigation")
      .getByRole("link", { name: "Użytkownicy", exact: true }),
  ).toBeVisible();
  expect((await page.request.get("/api/v1/admin/users/")).status()).toBe(200);
  await checkbox(page, "admin.rbac.manage").uncheck();
  await page.getByRole("button", { name: "Zapisz uprawnienia" }).click();
  await expect(
    page.getByText("Brak uprawnień", { exact: true }),
  ).toBeVisible();
  await expect(
    page
      .locator(".navigation")
      .getByRole("link", { name: "Role", exact: true }),
  ).toHaveCount(0);
  expect(
    (await page.request.get(`/api/v1/admin/rbac/roles/${roleID}`)).status(),
  ).toBe(403);
});

test("a transport error preserves organization edits and the retry updates context", async ({
  page,
}) => {
  await login(page);
  await page.goto("/tenant");
  const input = page.getByLabel("Nazwa organizacji");
  const original = await input.inputValue();
  const proposal = `Zmiana po błędzie ${scope()}`;
  await input.fill(proposal);
  await page.route("**/api/v1/admin/tenant/", (route) =>
    route.request().method() === "PUT"
      ? route.abort("failed")
      : route.continue(),
  );
  await page.getByRole("button", { name: "Zapisz zmianę" }).click();
  await expect(page.getByRole("alert")).toBeVisible();
  await expect(input).toHaveValue(proposal);
  await expect(
    page.getByRole("button", { name: "Zapisz zmianę" }),
  ).toBeEnabled();
  await page.unroute("**/api/v1/admin/tenant/");
  await page.getByRole("button", { name: "Zapisz zmianę" }).click();
  await expect(page.locator(".topbar")).toContainText(proposal);
  await input.fill(original);
  await page.getByRole("button", { name: "Zapisz zmianę" }).click();
  await expect(page.locator(".topbar")).toContainText(original);
});
