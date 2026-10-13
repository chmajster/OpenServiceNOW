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
const temporary = "request-temporary-password";
async function csrf(request: APIRequestContext) {
  return {
    "X-CSRF-Token": (await request.storageState()).cookies.find(
      (c) => c.name === "opensnow_csrf",
    )!.value,
  };
}
async function choose(page: Page, label: string, name: string) {
  const input = page.getByRole("combobox", { name: label, exact: true });
  await input.fill(name);
  await expect(page.getByRole("option", { name, exact: true })).toBeVisible();
  await input.press("ArrowDown");
  await input.press("Enter");
}
async function noOverflow(page: Page) {
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
}
test("published service creates once, retains snapshot and fulfills scoped tasks with private collaboration", async ({
  page,
  browser,
  request,
}, info) => {
  test.setTimeout(180000);
  page.setDefaultTimeout(10000);
  const scope = `${info.project.name}-${Date.now()}`;
  expect(
    (
      await loginAPI(request, {
        tenant,
        email: process.env.E2E_ADMIN_EMAIL ?? "admin",
        password: permanentRootPassword,
      })
    ).status(),
  ).toBe(200);
  const headers = await csrf(request);
  const groupName = `Realizacja ${scope}`;
  const groupResponse = await request.post("/api/v1/admin/rbac/groups", {
    headers,
    data: { name: groupName },
  });
  expect(groupResponse.status()).toBe(201);
  const group = (await groupResponse.json()).id as string;
  async function user(name: string, permissions: string[]) {
    const email = `request-${name}-${scope}@example.test`;
    const u = await request.post("/api/v1/admin/users/", {
      headers,
      data: { email, display_name: `${name} ${scope}`, password: temporary },
    });
    expect(u.status()).toBe(201);
    const id = (await u.json()).id as string;
    const role = await request.post("/api/v1/admin/rbac/roles", {
      headers,
      data: { name: `request-${name}-${scope}`, permissions },
    });
    expect(role.status()).toBe(201);
    expect(
      (
        await request.post(
          `/api/v1/admin/rbac/users/${id}/roles/${(await role.json()).id}`,
          { headers },
        )
      ).status(),
    ).toBe(204);
    return { id, email };
  }
  const reporter = await user("reporter", [
      "catalog.read",
      "request.create",
      "request.read_own",
      "request.cancel_own",
    ]),
    agent = await user("agent", ["request.fulfill"]);
  expect(
    (
      await request.post(
        `/api/v1/admin/rbac/groups/${group}/members/${agent.id}`,
        { headers },
      )
    ).status(),
  ).toBe(204);
  await page.context().addCookies((await request.storageState()).cookies);
  await page.goto("/catalog-admin");
  await expect(
    page.getByRole("heading", {
      name: "Administracja katalogiem",
      exact: true,
    }),
  ).toBeVisible();
  const catalogName = `Katalog ${scope}`,
    categoryName = `Kategoria ${scope}`,
    title = `Dostęp ${scope}`;
  await page.getByRole("button", { name: "Nowy katalog", exact: true }).click();
  await page.getByLabel("Nazwa", { exact: true }).fill(catalogName);
  await page.getByRole("button", { name: "Zapisz nazwę i opis" }).click();
  await expect(
    page.getByRole("button", { name: `Zmień ${catalogName}`, exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Nowa kategoria", exact: true })
    .click();
  await page.getByLabel("Nazwa", { exact: true }).fill(categoryName);
  await page
    .getByLabel("Katalog", { exact: true })
    .selectOption({ label: catalogName });
  await page.getByRole("button", { name: "Zapisz nazwę i opis" }).click();
  await expect(
    page.getByRole("button", { name: `Zmień ${categoryName}`, exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Szablon: Dostęp do systemu", exact: true })
    .click();
  await page
    .getByLabel("Kategoria", { exact: true })
    .selectOption({ label: categoryName });
  await page.getByLabel("Nazwa usługi").fill(title);
  await choose(page, "Grupa realizująca", groupName);
  await page
    .getByLabel("Instrukcja wewnętrzna")
    .fill("PRIVATE OPERATOR INSTRUCTION");
  for (const [index, type, label] of [
    [3, "checkbox", "Warunek"],
    [4, "number", "Liczba"],
    [5, "date", "Termin"],
    [6, "select", "Wariant"],
  ] as const) {
    await page.getByRole("button", { name: "Dodaj pole", exact: true }).click();
    const field = page.getByRole("group", {
      name: `Pole field_${index}`,
      exact: true,
    });
    await field
      .getByRole("combobox", { name: "Typ", exact: true })
      .selectOption(type);
    await field.getByLabel("Etykieta", { exact: true }).fill(label);
    if (type === "number") {
      await field
        .getByRole("combobox", { name: "Widoczne, gdy", exact: true })
        .selectOption("field_3");
      await field.getByLabel("Wymagane", { exact: true }).check();
    }
  }
  await page.getByRole("button", { name: "Zapisz szkic", exact: true }).click();
  await expect(page).toHaveURL(/catalog-admin\?.*id=/);
  const itemID = new URL(page.url()).searchParams.get("id")!;
  if (process.env.E2E_BIGINT === "1")
    expect(BigInt(itemID)).toBeGreaterThan(9007199254740991n);
  await page.getByRole("button", { name: "Opublikuj zapisaną wersję" }).click();
  await expect(page.getByText(/publikacja 1/)).toBeVisible();
  await noOverflow(page);
  await page.screenshot({
    path: info.outputPath("catalog-administration.png"),
    fullPage: true,
  });
  async function actorPage(email: string) {
    const context = await browser.newContext({
      viewport: page.viewportSize() ?? undefined,
    });
    const p = await context.newPage();
    expect(
      (
        await loginAPI(context.request, { tenant, email, password: temporary })
      ).status(),
    ).toBe(200);
    await p.goto("/account");
    await changeTemporaryPassword(p, temporary, personalPassword(temporary));
    return { context, page: p };
  }
  const reporting = await actorPage(reporter.email),
    operating = await actorPage(agent.email);
  const portal = reporting.page,
    operator = operating.page;
  await portal.goto(`/catalog/${itemID}`);
  await portal.getByLabel("System *").fill("ERP 🛠️");
  await portal.getByLabel("Uzasadnienie *").fill("Dostęp do raportów");
  await expect(portal.getByLabel("Liczba *")).toHaveCount(0);
  await portal.getByLabel("Warunek", { exact: true }).check();
  await portal.getByLabel("Liczba *").fill("1.5");
  await portal.getByLabel("Termin", { exact: true }).fill("2028-02-29");
  await portal
    .getByRole("combobox", { name: "Wariant", exact: true })
    .selectOption("Opcja 1");
  await portal.screenshot({
    path: info.outputPath("catalog-order.png"),
    fullPage: true,
  });
  let committed = "",
    number = "",
    key = "";
  await portal.route("**/api/v1/requests/", async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    key = route.request().headers()["idempotency-key"]!;
    const result = await route.fetch();
    expect(result.status()).toBe(201);
    const r = await result.json();
    committed = r.id;
    number = r.number;
    await route.abort("failed");
  });
  await portal
    .getByRole("button", { name: "Złóż wniosek", exact: true })
    .click();
  await expect(
    portal.getByText("Nie potwierdzono wyniku poprzedniej próby.", {
      exact: false,
    }),
  ).toBeVisible();
  await expect(portal.getByLabel("System *")).toHaveValue("ERP 🛠️");
  await portal.unroute("**/api/v1/requests/");
  const replay = portal.waitForRequest(
    (r) => r.url().endsWith("/api/v1/requests/") && r.method() === "POST",
  );
  await portal
    .getByRole("button", { name: "Potwierdź wynik poprzedniej próby" })
    .click();
  expect((await replay).headers()["idempotency-key"]).toBe(key);
  await expect(portal).toHaveURL(new RegExp(`/requests/${committed}$`));
  await expect(
    portal.getByRole("heading", { name: number, exact: true }),
  ).toBeVisible();
  await expect(
    portal.getByRole("heading", { name: number, exact: true }),
  ).toBeFocused();
  await portal.getByRole("tab", { name: "Informacje", exact: true }).focus();
  await portal.keyboard.press("ArrowRight");
  await expect(
    portal.getByRole("tab", { name: "Realizacja", exact: true }),
  ).toBeFocused();
  await expect(portal).toHaveURL(/tab=actions/);
  await portal.keyboard.press("Home");
  await expect(
    portal.getByRole("tab", { name: "Informacje", exact: true }),
  ).toBeFocused();
  expect(await portal.locator("body").innerText()).not.toContain("PRIVATE");
  // Publish a different required schema; the already ordered snapshot is unchanged.
  const current = await (
    await request.get(`/api/v1/catalog/admin/items/${itemID}`)
  ).json();
  current.draft.title = `Nowa ${title}`;
  current.draft.fields = [
    { key: "new", label: "Nowe wymagane pole", type: "text", required: true },
  ];
  const saved = await request.put(`/api/v1/catalog/admin/items/${itemID}`, {
    headers,
    data: {
      category_id: current.category_id,
      version: current.version,
      draft: current.draft,
    },
  });
  expect(saved.status()).toBe(200);
  expect(
    (
      await request.post(`/api/v1/catalog/admin/items/${itemID}/publish`, {
        headers,
        data: { version: (await saved.json()).version },
      })
    ).status(),
  ).toBe(200);
  await portal.reload();
  await expect(portal.getByText(/Wersja zamówionej usługi: 1/)).toBeVisible();
  await expect(portal.getByText("ERP 🛠️", { exact: true })).toBeVisible();
  expect(await portal.locator("body").innerText()).not.toContain(
    "Nowe wymagane pole",
  );
  const queue = await (
    await operating.context.request.get(
      "/api/v1/fulfillment/?view=group&limit=100",
    )
  ).json();
  const task = queue.items.find(
    (t: { request_id: string }) => t.request_id === committed,
  );
  expect(task).toBeTruthy();
  const before = await (
    await request.get(`/api/v1/requests/${committed}`)
  ).json();
  expect(
    (
      await request.post(`/api/v1/requests/${committed}/transition`, {
        headers,
        data: { version: before.version, state: "FULFILLED" },
      })
    ).status(),
  ).toBe(409);
  await operator.goto("/fulfillment");
  await operator.getByRole("link", { name: task.number, exact: true }).click();
  await operator.getByRole("tab", { name: "Realizacja", exact: true }).click();
  await operator
    .getByRole("button", { name: "Przypisz do siebie", exact: true })
    .click();
  await expect(
    operator.getByRole("combobox", { name: "Realizujący", exact: true }),
  ).toHaveValue(`agent ${scope}`);
  await operator.getByLabel("Nowy stan").selectOption("IN_PROGRESS");
  await operator
    .getByRole("button", { name: "Zapisz stan", exact: true })
    .click();
  await expect(operator.locator(".record-facts")).toContainText("W realizacji");
  await operator
    .getByRole("tab", { name: "Komunikacja i pliki", exact: true })
    .click();
  await operator
    .getByRole("button", { name: "Notatka wewnętrzna", exact: true })
    .click();
  await operator.getByLabel("Treść notatki wewnętrznej").fill("PRIVATE NOTE");
  await operator
    .getByRole("button", { name: "Zapisz notatkę wewnętrzną", exact: true })
    .click();
  await expect(
    operator.getByText("PRIVATE NOTE", { exact: true }),
  ).toBeVisible();
  await operator.getByLabel("Plik (do 10 MiB)").setInputFiles({
    name: "private-request.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("PRIVATE FILE"),
  });
  await operator
    .getByRole("button", { name: "Prześlij plik", exact: true })
    .click();
  await expect(
    operator.getByRole("button", {
      name: "Pobierz private-request.txt",
      exact: true,
    }),
  ).toBeVisible();
  const privateFiles = await (
    await operating.context.request.get(`/api/v1/requests/${committed}/files`)
  ).json();
  const privateFile = privateFiles.files.find(
    (f: { file_name: string }) => f.file_name === "private-request.txt",
  );
  expect(
    (
      await reporting.context.request.get(
        `/api/v1/requests/${committed}/files/${privateFile.id}`,
      )
    ).status(),
  ).toBe(404);
  await operator
    .getByRole("button", { name: "Odpowiedź dla zgłaszającego", exact: true })
    .click();
  await operator.getByLabel("Treść odpowiedzi").fill("Przygotowujemy dostęp");
  await operator
    .getByRole("button", { name: "Wyślij odpowiedź", exact: true })
    .click();
  await expect(
    operator.getByText("Przygotowujemy dostęp", { exact: true }),
  ).toBeVisible();
  await operator.getByLabel("Plik (do 10 MiB)").setInputFiles({
    name: "public-request.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("Public request file"),
  });
  await operator
    .getByRole("button", { name: "Prześlij plik", exact: true })
    .click();
  await expect(
    operator.getByRole("button", {
      name: "Pobierz public-request.txt",
      exact: true,
    }),
  ).toBeVisible();
  await portal
    .getByRole("tab", { name: "Komunikacja i pliki", exact: true })
    .click();
  await portal
    .getByRole("button", { name: "Odśwież komunikację", exact: true })
    .click();
  await expect(
    portal.getByText("Przygotowujemy dostęp", { exact: true }),
  ).toBeVisible();
  expect(await portal.locator("body").innerText()).not.toContain(
    "PRIVATE NOTE",
  );
  await expect(
    portal.getByRole("button", {
      name: "Pobierz private-request.txt",
      exact: true,
    }),
  ).toHaveCount(0);
  const downloadEvent = portal.waitForEvent("download");
  await portal
    .getByRole("button", { name: "Pobierz public-request.txt", exact: true })
    .click();
  expect((await downloadEvent).suggestedFilename()).toBe("public-request.txt");
  await operator.getByRole("tab", { name: "Realizacja", exact: true }).click();
  await operator.getByLabel("Nowy stan").selectOption("COMPLETED");
  await operator
    .getByLabel("Opis wyniku lub uzasadnienie")
    .fill("Zrealizowano dostęp");
  await operator
    .getByRole("button", { name: "Zapisz stan", exact: true })
    .click();
  await expect(operator.locator(".record-facts")).toContainText("Ukończone");
  await operator.goto(`/requests/${committed}`);
  await operator.getByRole("tab", { name: "Realizacja", exact: true }).click();
  await operator.getByLabel("Nowy stan").selectOption("FULFILLED");
  await operator
    .getByRole("button", { name: "Zapisz stan", exact: true })
    .click();
  await operator.getByLabel("Nowy stan").selectOption("CLOSED");
  await operator
    .getByRole("button", { name: "Zapisz stan", exact: true })
    .click();
  await expect(operator.locator(".record-facts")).toContainText("Zamknięty");
  await portal.reload();
  await expect(portal.locator(".record-facts")).toContainText("Zamknięty");
  await portal.getByRole("tab", { name: "Historia", exact: true }).click();
  await expect
    .poll(
      async () => {
        await portal
          .getByRole("button", { name: "Odśwież historię", exact: true })
          .click();
        try {
          await portal
            .getByText("Złożono wniosek", { exact: true })
            .waitFor({ state: "visible", timeout: 1000 });
        } catch {
          return 0;
        }
        return await portal
          .getByText("Złożono wniosek", { exact: true })
          .count();
      },
      { timeout: 15000 },
    )
    .toBe(1);
  expect(await portal.locator("body").innerText()).not.toContain("PRIVATE");
  await noOverflow(portal);
  await noOverflow(operator);
  await portal.screenshot({
    path: info.outputPath("request-history.png"),
    fullPage: true,
  });
  await operator.screenshot({
    path: info.outputPath("request-fulfillment.png"),
    fullPage: true,
  });
  await reporting.context.close();
  await operating.context.close();
});

test("catalog and task conflicts retain the local proposal and require an explicit decision", async ({
  page,
  request,
}, info) => {
  test.setTimeout(90000);
  page.setDefaultTimeout(10000);
  const scope = `conflict-${info.project.name}-${Date.now()}`;
  expect(
    (
      await loginAPI(request, {
        tenant,
        email: process.env.E2E_ADMIN_EMAIL ?? "admin",
        password: permanentRootPassword,
      })
    ).status(),
  ).toBe(200);
  const headers = await csrf(request);
  async function create(path: string, data: unknown) {
    const response = await request.post(path, {
      headers: { ...headers, "Idempotency-Key": scope + crypto.randomUUID() },
      data,
    });
    expect(response.status()).toBe(200);
    return response.json();
  }
  const catalogs = await create("/api/v1/catalog/admin/catalogs", {
    name: scope,
    description: "",
    version: "0",
  });
  const category = await create("/api/v1/catalog/admin/categories", {
    catalog_id: catalogs.id,
    name: scope,
    description: "",
    version: "0",
  });
  const groupResponse = await request.post("/api/v1/admin/rbac/groups", {
    headers,
    data: { name: scope },
  });
  expect(groupResponse.status()).toBe(201);
  const group = (await groupResponse.json()).id;
  const me = await (await request.get("/api/v1/auth/me")).json();
  expect(
    (
      await request.post(
        `/api/v1/admin/rbac/groups/${group}/members/${me.user.id}`,
        { headers },
      )
    ).status(),
  ).toBe(204);
  const original = {
    title: scope,
    description: "",
    method: "MANUAL_TASKS",
    group_id: group,
    fields: [{ key: "reason", label: "Opis", type: "text", required: true }],
    steps: [{ title: "Realizacja", instructions: "", required: true }],
  };
  let item = await create("/api/v1/catalog/admin/items", {
    category_id: category.id,
    version: "0",
    draft: original,
  });
  item = await create(`/api/v1/catalog/admin/items/${item.id}/publish`, {
    version: item.version,
  });
  await page.context().addCookies((await request.storageState()).cookies);
  await page.goto(`/catalog-admin?id=${item.id}`);
  await page.getByLabel("Nazwa usługi").fill(`Moja propozycja ${scope}`);
  const remote = await request.put(`/api/v1/catalog/admin/items/${item.id}`, {
    headers,
    data: {
      category_id: category.id,
      version: item.version,
      draft: { ...original, title: `Zdalna ${scope}` },
    },
  });
  expect(remote.status()).toBe(200);
  await page.getByRole("button", { name: "Zapisz szkic", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Konflikt wersji", exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("Nazwa usługi")).toHaveValue(
    `Moja propozycja ${scope}`,
  );
  await page
    .getByRole("button", {
      name: "Zachowaj propozycję, zapisz na aktualnej wersji",
    })
    .click();
  await page.getByRole("button", { name: "Zapisz szkic", exact: true }).click();
  await expect(
    page.getByText("Zapisano. Historia zostanie uzupełniona przez worker.", {
      exact: true,
    }),
  ).toBeVisible();
  const submission = await request.post("/api/v1/requests/", {
    headers: { ...headers, "Idempotency-Key": scope + crypto.randomUUID() },
    data: {
      items: [
        {
          item_id: item.id,
          version: item.published_version,
          values: { reason: "Test" },
        },
      ],
    },
  });
  expect(submission.status()).toBe(201);
  const ordered = await submission.json();
  const queue = await (
    await request.get(`/api/v1/fulfillment/?q=${scope}&view=group&limit=100`)
  ).json();
  const all = queue.items.length
    ? queue.items
    : (
        await (
          await request.get("/api/v1/fulfillment/?view=group&limit=100")
        ).json()
      ).items;
  const task = all.find(
    (t: { request_id: string }) => t.request_id === ordered.id,
  );
  expect(task).toBeTruthy();
  const assigned = await request.put(
    `/api/v1/fulfillment/${task.id}/assignment`,
    { headers, data: { version: task.version, assigned_to: me.user.id } },
  );
  expect(assigned.status()).toBe(200);
  const current = await assigned.json();
  await page.goto(`/fulfillment/${task.id}?tab=actions`);
  await page.getByLabel("Nowy stan").selectOption("IN_PROGRESS");
  await page
    .getByLabel("Opis wyniku lub uzasadnienie")
    .fill("Lokalne uzasadnienie pozostaje");
  expect(
    (
      await request.put(`/api/v1/fulfillment/${task.id}/assignment`, {
        headers,
        data: { version: current.version, assigned_to: me.user.id },
      })
    ).status(),
  ).toBe(200);
  await page.getByRole("button", { name: "Zapisz stan", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Konflikt wersji", exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("Opis wyniku lub uzasadnienie")).toHaveValue(
    "Lokalne uzasadnienie pozostaje",
  );
  await page
    .getByRole("button", {
      name: "Zachowaj propozycję i użyj aktualnej wersji",
    })
    .click();
  await page.getByRole("button", { name: "Zapisz stan", exact: true }).click();
  await expect(page.locator(".record-facts")).toContainText("W realizacji");
  await noOverflow(page);
});
