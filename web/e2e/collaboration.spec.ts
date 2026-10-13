import {
  expect,
  test,
  type APIRequestContext,
  type Page,
} from "@playwright/test";
import {
  loginAPI,
  permanentRootPassword,
  changeTemporaryPassword,
  personalPassword,
} from "./account-fixtures";

const tenant = process.env.E2E_TENANT ?? "e2e";
async function csrf(request: APIRequestContext) {
  return {
    "X-CSRF-Token": (await request.storageState()).cookies.find(
      (c) => c.name === "opensnow_csrf",
    )!.value,
  };
}
async function signIn(page: Page, email: string, password: string) {
  await page.goto("/login");
  await page.getByLabel("Organizacja", { exact: true }).fill(tenant);
  await page.getByLabel("Login lub e-mail").fill(email);
  await page.getByLabel("Hasło", { exact: true }).fill(password);
  async function submit() {
    const response = page.waitForResponse(
      (r) =>
        r.url().endsWith("/api/v1/auth/login") &&
        r.request().method() === "POST",
    );
    await page.getByRole("button", { name: "Zaloguj", exact: true }).click();
    return response;
  }
  let response = await submit();
  if (response.status() === 429) {
    const seconds = Number(response.headers()["retry-after"]);
    expect(seconds).toBeGreaterThan(0);
    expect(seconds).toBeLessThanOrEqual(60);
    await new Promise((resolve) => setTimeout(resolve, seconds * 1000));
    response = await submit();
  }
  expect(response.status()).toBe(200);
  await expect(page).not.toHaveURL(/\/login(?:\?|$)/);
}
async function choose(page: Page, label: string, search: string) {
  const combo = page.getByRole("combobox", { name: label, exact: true });
  await combo.fill(search);
  await expect(page.getByRole("option").first()).toBeVisible();
  await combo.press("ArrowDown");
  await combo.press("Enter");
}

test("incident communication protects notes/files and supports watchers, relations and operator queue", async ({
  page,
  browser,
  request,
}, info) => {
  test.setTimeout(180_000);
  page.setDefaultTimeout(10_000);
  expect(
    (
      await loginAPI(request, {
        tenant,
        email: process.env.E2E_ADMIN_EMAIL ?? "admin",
        password: permanentRootPassword,
      })
    ).status(),
  ).toBe(200);
  const headers = await csrf(request),
    scope = `collab-${info.project.name}-${Date.now()}`,
    email = `${scope}@example.test`,
    temporary = "collaboration-temporary-password";
  const created = await request.post("/api/v1/admin/users/", {
    headers,
    data: { email, display_name: `Reporter ${scope}`, password: temporary },
  });
  expect(created.status()).toBe(201);
  const userID = (await created.json()).id;
  expect(typeof userID).toBe("string");
  if (process.env.E2E_BIGINT === "1")
    expect(BigInt(userID)).toBeGreaterThan(9007199254740991n);
  const role = await request.post("/api/v1/admin/rbac/roles", {
    headers,
    data: {
      name: scope,
      permissions: [
        "incident.create",
        "incident.read_own",
        "incident.communicate",
      ],
    },
  });
  expect(role.status()).toBe(201);
  expect(
    (
      await request.post(
        `/api/v1/admin/rbac/users/${userID}/roles/${(await role.json()).id}`,
        { headers },
      )
    ).status(),
  ).toBe(204);
  const reporterContext = await browser.newContext({
    baseURL: process.env.E2E_BASE_URL ?? "http://127.0.0.1:5173",
    ...(info.project.name === "mobile-chromium"
      ? {
          viewport: { width: 412, height: 915 },
          isMobile: true,
          hasTouch: true,
        }
      : {}),
  });
  const reporter = await reporterContext.newPage();
  reporter.setDefaultTimeout(10_000);
  try {
    await signIn(reporter, email, temporary);
    await changeTemporaryPassword(
      reporter,
      temporary,
      personalPassword(temporary),
    );
    const fields = {
      title: `Komunikacja ${scope}`,
      description: "Test przepływu komunikacji",
      category: "other",
      subcategory: "other",
      impact: 2,
      urgency: 2,
    };
    const incident = await reporter.request.post("/api/v1/incidents/", {
      headers: { ...(await csrf(reporter.request)), "Idempotency-Key": scope },
      data: fields,
    });
    expect(incident.status()).toBe(201);
    const record = await incident.json(),
      base = `/api/v1/incidents/${record.id}`;
    await signIn(
      page,
      process.env.E2E_ADMIN_EMAIL ?? "admin",
      permanentRootPassword,
    );
    await page.goto(`/incidents/${record.id}?tab=communication`);
    await page
      .getByRole("button", { name: "Notatka wewnętrzna", exact: true })
      .click();
    await page
      .getByLabel("Treść notatki wewnętrznej")
      .fill(`TAJNA-NOTATKA-${scope}`);
    await expect(page.getByLabel("Kategoria dostępu")).toHaveValue("INTERNAL");
    await page
      .getByRole("button", { name: "Odpowiedź dla zgłaszającego", exact: true })
      .click();
    await expect(page.getByLabel("Treść odpowiedzi")).toHaveValue("");
    await expect(page.getByLabel("Kategoria dostępu")).toHaveValue("PUBLIC");
    await page
      .getByRole("button", { name: "Notatka wewnętrzna", exact: true })
      .click();
    await expect(page.getByLabel("Treść notatki wewnętrznej")).toHaveValue(
      `TAJNA-NOTATKA-${scope}`,
    );
    const navigationLink = page
      .getByRole("navigation", { name: "Główna nawigacja" })
      .getByRole("link", { name: "Incydenty", exact: true });
    if (!(await navigationLink.isVisible()))
      await page
        .getByRole("button", { name: "Rozwiń nawigację", exact: true })
        .click();
    await navigationLink.click();
    await expect(page.getByRole("alertdialog")).toBeVisible();
    await page
      .getByRole("button", { name: "Pozostań na stronie" })
      .press("Escape");
    await expect(page.getByLabel("Treść notatki wewnętrznej")).toHaveValue(
      `TAJNA-NOTATKA-${scope}`,
    );
    expect(
      await page.evaluate(() =>
        Object.values(localStorage).some((value) =>
          String(value).includes("TAJNA-NOTATKA"),
        ),
      ),
    ).toBe(false);
    await page
      .getByRole("button", { name: "Zapisz notatkę wewnętrzną" })
      .click();
    await expect(
      page.getByRole("status").filter({ hasText: "Wiadomość zapisana" }),
    ).toBeVisible();
    await page.getByLabel("Plik (do 10 MiB)").setInputFiles({
      name: `tajny-${scope}.txt`,
      mimeType: "text/plain",
      buffer: Buffer.from("TAJNE-BAJTY"),
    });
    await page
      .getByRole("button", { name: "Odpowiedź dla zgłaszającego", exact: true })
      .click();
    await expect(page.getByLabel("Kategoria dostępu")).toHaveValue("INTERNAL");
    await page
      .getByRole("button", { name: "Notatka wewnętrzna", exact: true })
      .click();
    const privateUpload = page.waitForResponse(
      (r) =>
        r.url().endsWith(base + "/files") && r.request().method() === "POST",
    );
    await page
      .getByRole("button", { name: "Prześlij plik", exact: true })
      .click();
    const privateResponse = await privateUpload;
    expect(privateResponse.status()).toBe(200);
    expect(privateResponse.request().headers()["content-type"]).toMatch(
      /^multipart\/form-data; boundary=/,
    );
    const privateFile = await privateResponse.json();
    expect(privateFile.visibility).toBe("INTERNAL");
    expect(privateFile.storage_key).toBeUndefined();
    await expect(
      page.getByRole("button", { name: `Pobierz ${privateFile.file_name}` }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Odpowiedź dla zgłaszającego", exact: true })
      .click();
    await page
      .getByLabel("Treść odpowiedzi")
      .fill(`PUBLICZNA-ODPOWIEDZ-${scope}`);
    let messageKey: string | undefined;
    await page.route(`**${base}/messages`, async (route) => {
      if (route.request().method() !== "POST") {
        await route.continue();
        return;
      }
      messageKey = route.request().headers()["idempotency-key"];
      expect((await route.fetch()).status()).toBe(200);
      await route.abort("connectionfailed");
    });
    await page
      .getByRole("button", { name: "Wyślij odpowiedź", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "Sprawdź zapis wiadomości" }),
    ).toBeVisible();
    await expect(page.getByLabel("Treść odpowiedzi")).toHaveValue(
      `PUBLICZNA-ODPOWIEDZ-${scope}`,
    );
    await page.unroute(`**${base}/messages`);
    const retry = page.waitForResponse(
      (r) =>
        r.url().endsWith(base + "/messages") && r.request().method() === "POST",
    );
    await page
      .getByRole("button", { name: "Sprawdź zapis wiadomości" })
      .click();
    expect((await retry).request().headers()["idempotency-key"]).toBe(
      messageKey,
    );
    await page.getByLabel("Plik (do 10 MiB)").setInputFiles({
      name: `publiczny-${scope}.txt`,
      mimeType: "text/plain",
      buffer: Buffer.from("PUBLICZNE-BAJTY"),
    });
    const publicUpload = page.waitForResponse(
      (r) =>
        r.url().endsWith(base + "/files") && r.request().method() === "POST",
    );
    await page
      .getByRole("button", { name: "Prześlij plik", exact: true })
      .click();
    const publicFile = await (await publicUpload).json();
    expect(publicFile.visibility).toBe("PUBLIC");
    const downloaded = page.waitForEvent("download");
    await page
      .getByRole("button", { name: `Pobierz ${publicFile.file_name}` })
      .click();
    expect((await downloaded).suggestedFilename()).toBe(publicFile.file_name);
    await expect
      .poll(
        async () =>
          (await (await page.request.get(base + "/history")).json()).pending,
        { timeout: 20_000 },
      )
      .toBe(false);
    await reporter.goto(`/incidents/${record.id}?tab=communication`);
    await expect(
      reporter.getByText(`PUBLICZNA-ODPOWIEDZ-${scope}`, { exact: true }),
    ).toBeVisible();
    await expect(
      reporter.getByText(`TAJNA-NOTATKA-${scope}`, { exact: true }),
    ).toHaveCount(0);
    await expect(
      reporter.getByRole("button", { name: "Notatka wewnętrzna", exact: true }),
    ).toHaveCount(0);
    for (const suffix of [
      "",
      "/messages?limit=1",
      "/files?limit=1",
      "/history?limit=1",
      "/watchers",
      "/relations",
    ]) {
      const response = await reporter.request.get(base + suffix);
      expect(response.status()).toBe(200);
      expect(await response.text()).not.toContain("TAJNA-NOTATKA");
      expect(await response.text()).not.toContain(privateFile.file_name);
    }
    expect(
      (await reporter.request.get(`${base}/files/${privateFile.id}`)).status(),
    ).toBe(404);
    expect(
      (await reporter.request.get(`${base}/files/${publicFile.id}`)).status(),
    ).toBe(200);
    await reporter
      .getByLabel("Treść odpowiedzi")
      .fill(`Reporter odpowiada ${scope}`);
    await reporter
      .getByRole("button", { name: "Wyślij odpowiedź", exact: true })
      .click();
    await expect(
      reporter.getByRole("status").filter({ hasText: "Wiadomość zapisana" }),
    ).toBeVisible();
    await reporter.reload();
    await expect(
      reporter.getByText(`Reporter odpowiada ${scope}`, { exact: true }),
    ).toBeVisible();
    await page
      .getByRole("link", { name: "Obserwatorzy i powiązania", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Obserwuj incydent", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "Przestań obserwować", exact: true }),
    ).toBeVisible();
    await choose(page, "Nowy obserwator", email);
    await page
      .getByRole("button", { name: "Dodaj obserwatora", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: `Usuń obserwatora Reporter ${scope}` }),
    ).toBeVisible();
    const target = await request.post("/api/v1/incidents/", {
      headers: { ...headers, "Idempotency-Key": `${scope}-target` },
      data: { ...fields, title: `Cel ${scope}` },
    });
    expect(target.status()).toBe(201);
    const related = await target.json();
    await choose(page, "Powiązany incydent", related.number);
    await page.getByLabel("Rodzaj relacji").selectOption("PARENT");
    await page
      .getByRole("button", { name: "Dodaj powiązanie", exact: true })
      .click();
    await expect(
      page.getByRole("link", { name: related.number, exact: true }),
    ).toBeVisible();
    expect((await reporter.request.get(base + "/relations")).status()).toBe(
      200,
    );
    expect(
      (await (await reporter.request.get(base + "/relations")).json())
        .relations,
    ).toHaveLength(0);
    await page.goto(`/incidents?queue=1&q=${encodeURIComponent(scope)}`);
    await expect(
      page.getByRole("heading", { name: "Kolejka operatora", exact: true }),
    ).toBeVisible();
    await page.getByLabel(`Wybierz ${record.number}`, { exact: true }).check();
    page.once("dialog", (dialog) => void dialog.accept());
    await page
      .getByRole("button", { name: "Przypisz wybrane do siebie", exact: true })
      .click();
    await expect(
      page.getByRole("status").filter({ hasText: "Wyniki operacji" }),
    ).toContainText("Zapisano");
    await expect(page).toHaveURL(/q=/);
    await page.getByLabel(`Wybierz ${record.number}`, { exact: true }).check();
    page.once("dialog", (dialog) => void dialog.accept());
    await page
      .getByRole("button", {
        name: "Rozpocznij obsługę wybranych",
        exact: true,
      })
      .click();
    await expect
      .poll(async () => (await (await request.get(base)).json()).state)
      .toBe("IN_PROGRESS");
    await page.goto(`/incidents/${record.id}?tab=communication`);
    page.once("dialog", (dialog) => void dialog.accept());
    await page
      .getByRole("button", { name: `Usuń ${publicFile.file_name}` })
      .click();
    await expect(
      page.getByRole("button", { name: `Pobierz ${publicFile.file_name}` }),
    ).toHaveCount(0);
    expect(
      (await reporter.request.get(`${base}/files/${publicFile.id}`)).status(),
    ).toBe(404);
    await page.getByRole("link", { name: "Historia", exact: true }).click();
    await page.getByRole("button", { name: "Odśwież historię" }).click();
    await expect(
      page
        .getByRole("list", { name: "Historia zdarzeń" })
        .getByText(`TAJNA-NOTATKA-${scope}`, { exact: true }),
    ).toBeVisible();
    await expect(
      page
        .getByRole("list", { name: "Historia zdarzeń" })
        .getByText(`PUBLICZNA-ODPOWIEDZ-${scope}`, { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("list", { name: "Historia zdarzeń" }),
    ).not.toContainText("Pundefined");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: info.outputPath("collaboration-history.png"),
      fullPage: true,
    });
  } finally {
    await reporterContext.close();
  }
});
