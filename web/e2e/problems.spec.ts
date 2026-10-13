import { expect, test, type Page } from "@playwright/test";
import { loginAPI, permanentRootPassword } from "./account-fixtures";
async function choose(page: Page, label: string, value: string) {
  const input = page.getByRole("combobox", { name: label, exact: true });
  await input.fill(value);
  await expect(
    page.getByRole("option", { name: value, exact: true }),
  ).toBeVisible();
  await input.press("ArrowDown");
  await input.press("Enter");
}
test("Problem joins incidents, CI, private communication and controlled analytical work", async ({
  page,
}, info) => {
  test.setTimeout(150000);
  page.setDefaultTimeout(10000);
  expect(
    (
      await loginAPI(page.request, {
        tenant: process.env.E2E_TENANT ?? "e2e",
        email: process.env.E2E_ADMIN_EMAIL ?? "admin",
        password: permanentRootPassword,
      })
    ).status(),
  ).toBe(200);
  const me = await (await page.request.get("/api/v1/auth/me")).json(),
    headers = {
      "X-CSRF-Token": (await page.request.storageState()).cookies.find(
        (c) => c.name === "opensnow_csrf",
      )!.value,
    };
  const keyed = () => ({ ...headers, "Idempotency-Key": crypto.randomUUID() }),
    suffix = `${info.project.name}-${Date.now()}`,
    name = `Problem ${suffix}`,
    groupName = `Analiza ${suffix}`;
  const group = await page.request.post("/api/v1/admin/rbac/groups", {
    headers,
    data: { name: groupName },
  });
  expect(group.status()).toBe(201);
  const created = await page.request.post("/api/v1/incidents/", {
    headers: keyed(),
    data: {
      title: name,
      description: "Powtarzalne przerwy połączenia",
      category: "software",
      subcategory: "application",
      impact: 1,
      urgency: 2,
    },
  });
  expect(created.status()).toBe(201);
  const inc = await created.json();
  expect(
    (
      await page.request.post(`/api/v1/incidents/${inc.id}/messages`, {
        headers: keyed(),
        data: { type: "WORK_NOTE", body: `Poufne dane incydentu ${suffix}` },
      })
    ).status(),
  ).toBe(200);
  const ciResponse = await page.request.post("/api/v1/cmdb/cis", {
    headers: keyed(),
    data: {
      identifier: `problem-ci-${suffix}`,
      name: `CI ${suffix}`,
      class: "DATABASE",
      status: "ACTIVE",
      owner_id: me.user.id,
      environment: "PROD",
      attributes: { engine: "PostgreSQL" },
    },
  });
  expect(ciResponse.status()).toBe(200);
  const ci = await ciResponse.json();
  await page.goto(`/incidents/${inc.id}?tab=problems`);
  await page
    .getByRole("link", { name: "Utwórz problem z incydentu", exact: true })
    .click();
  const preview = page.getByRole("heading", {
    name: "Podgląd informacji z incydentu",
  });
  await expect(preview).toBeVisible();
  await expect(page.locator("main")).not.toContainText(
    `Poufne dane incydentu ${suffix}`,
  );
  await page
    .getByRole("button", {
      name: "Użyj pokazanych informacji i powiąż incydent",
    })
    .click();
  await choose(page, "Grupa odpowiedzialna", groupName);
  let lost = false;
  await page.route("**/api/v1/problems/", async (route) => {
    if (route.request().method() === "POST" && !lost) {
      lost = true;
      await route.fetch();
      await route.abort("failed");
    } else await route.continue();
  });
  await page
    .getByRole("button", { name: "Utwórz problem", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Potwierdź wynik zapisu", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Potwierdź wynik zapisu", exact: true })
    .click();
  await expect(page).toHaveURL(/\/problems\/[0-9]+$/);
  await page.unroute("**/api/v1/problems/");
  const id = new URL(page.url()).pathname.split("/").at(-1)!;
  if (process.env.E2E_ASSERT_BIGINT)
    expect(BigInt(id)).toBeGreaterThan(9007199254740991n);
  expect(
    (
      await (
        await page.request.get(
          `/api/v1/problems/?q=${encodeURIComponent(name)}`,
        )
      ).json()
    ).total,
  ).toBe("1");
  expect(
    (await (await page.request.get(`/api/v1/problems/${id}/messages`)).json())
      .messages,
  ).toHaveLength(0);
  await page
    .getByRole("tab", { name: "Powiązane rekordy", exact: true })
    .click();
  await expect(
    page.getByRole("link", { name: new RegExp(inc.number) }).first(),
  ).toBeVisible();
  await choose(page, "Element konfiguracji", ci.name);
  await page.getByRole("button", { name: "Powiąż CI", exact: true }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "Zapisano powiązanie CI." }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: new RegExp(ci.name) }).first(),
  ).toBeVisible();
  await page.goto(`/cmdb/${ci.id}?tab=records`);
  await expect(
    page.getByRole("link", { name: new RegExp(name) }).first(),
  ).toBeVisible();
  await page.goto(`/problems/${id}`);
  async function transition(state: string, label: string) {
    await page
      .getByLabel("Przejście procesu", { exact: true })
      .selectOption(state);
    await page
      .getByRole("button", { name: "Wykonaj przejście", exact: true })
      .click();
    await expect(page.locator(".record-facts")).toContainText(label);
  }
  await transition("ASSESS", "Ocena");
  await page
    .getByLabel("Objawy", { exact: true })
    .fill("Timeout pod obciążeniem");
  await page
    .getByRole("button", { name: "Zapisz problem", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Zapisz problem", exact: true }),
  ).toBeDisabled();
  await transition("ROOT_CAUSE", "Analiza przyczyny");
  await page
    .getByLabel("Wyniki analizy", { exact: true })
    .fill("Odtworzono wyczerpanie puli");
  await page
    .getByLabel("Przyczyna źródłowa", { exact: true })
    .fill("Nieograniczone połączenia");
  await page.getByLabel("Obejście", { exact: true }).fill("Ograniczenie ruchu");
  await page
    .getByLabel("Plan trwałego rozwiązania", { exact: true })
    .fill("Limit puli i test obciążenia");
  await page.getByLabel("Known Error", { exact: false }).check();
  await page
    .getByRole("button", { name: "Zapisz problem", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Zapisz problem", exact: true }),
  ).toBeDisabled();
  await transition("FIX_IN_PROGRESS", "Przygotowanie rozwiązania");

  await page
    .getByLabel("Opis skrócony", { exact: true })
    .fill(`${name} propozycja`);
  const before = await (
    await page.request.get(`/api/v1/problems/${id}`)
  ).json();
  const allowed = [
    "title",
    "description",
    "impact",
    "symptoms",
    "analysis",
    "root_cause",
    "workaround",
    "solution_plan",
    "verification",
    "known_error",
    "assigned_to",
    "assignment_group_id",
  ];
  expect(
    (
      await page.request.put(`/api/v1/problems/${id}`, {
        headers: keyed(),
        data: {
          ...Object.fromEntries(allowed.map((k) => [k, before[k]])),
          title: `${name} zdalny`,
          version: before.version,
        },
      })
    ).status(),
  ).toBe(200);
  await page
    .getByRole("button", { name: "Odśwież problem", exact: true })
    .click();
  await expect(page.getByLabel("Opis skrócony", { exact: true })).toHaveValue(
    `${name} propozycja`,
  );
  await page
    .getByRole("button", { name: "Zapisz problem", exact: true })
    .click();
  await expect(
    page.getByRole("region", { name: "Konflikt wersji" }),
  ).toContainText(`${name} zdalny`);
  await page
    .getByRole("button", {
      name: "Zachowaj propozycję i użyj aktualnej wersji",
      exact: true,
    })
    .click();
  await page
    .getByRole("button", { name: "Zapisz problem", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Zapisz problem", exact: true }),
  ).toBeDisabled();
  await page.getByRole("tab", { name: "Aktywność", exact: true }).click();
  await page
    .getByRole("button", { name: "Notatka wewnętrzna", exact: true })
    .click();
  await page
    .getByLabel("Treść notatki wewnętrznej", { exact: true })
    .fill(`Prywatna analiza ${suffix}`);
  await page
    .getByRole("button", { name: "Zapisz notatkę wewnętrzną", exact: true })
    .click();
  await expect(page.locator(".incident-timeline")).toContainText(
    `Prywatna analiza ${suffix}`,
  );
  await page.getByRole("tab", { name: "Zadania", exact: true }).click();
  await page
    .getByRole("button", { name: "Nowe zadanie analityczne", exact: true })
    .click();
  await page
    .getByLabel("Opis zadania", { exact: true })
    .fill("Sprawdzenie puli");
  await page
    .getByLabel("Instrukcje analizy", { exact: true })
    .fill("Test 100 równoległych sesji");
  await page
    .getByRole("button", { name: "Utwórz zadanie analityczne", exact: true })
    .click();
  await expect(page.locator(".record-table")).toContainText("Sprawdzenie puli");
  if (process.env.E2E_ASSERT_BIGINT) {
    const tasks = await (
      await page.request.get(`/api/v1/problems/${id}/tasks`)
    ).json();
    expect(BigInt(tasks.items[0].id)).toBeGreaterThan(9007199254740991n);
  }
  await page
    .getByRole("button", { name: "Otwórz zadanie", exact: true })
    .click();
  await page
    .getByLabel("Stan zadania analitycznego", { exact: true })
    .selectOption("IN_PROGRESS");
  const concurrentParent = await (
    await page.request.get(`/api/v1/problems/${id}`)
  ).json();
  expect(
    (
      await page.request.put(`/api/v1/problems/${id}`, {
        headers: keyed(),
        data: {
          ...Object.fromEntries(allowed.map((k) => [k, concurrentParent[k]])),
          version: concurrentParent.version,
          analysis: "Nowe ustalenie zapisane równolegle",
        },
      })
    ).status(),
  ).toBe(200);
  await page
    .getByRole("button", { name: "Zapisz zadanie", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Konflikt wersji zadania", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByLabel("Stan zadania analitycznego", { exact: true }),
  ).toHaveValue("IN_PROGRESS");
  await page
    .getByRole("button", { name: "Zachowaj propozycję zadania", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Konflikt wersji zadania", exact: true }),
  ).toHaveCount(0);
  await page
    .getByRole("button", { name: "Zapisz zadanie", exact: true })
    .click();
  await expect(page.locator(".record-table")).toContainText("W realizacji");
  await page
    .getByRole("button", { name: "Otwórz zadanie", exact: true })
    .click();
  await page
    .getByLabel("Stan zadania analitycznego", { exact: true })
    .selectOption("COMPLETED");
  await page
    .getByLabel("Wynik zadania", { exact: true })
    .fill("100 sesji bez przekroczenia limitu");
  await page
    .getByRole("button", { name: "Zapisz zadanie", exact: true })
    .click();
  await expect(page.locator(".record-table")).toContainText("Ukończone");
  await page
    .getByRole("button", { name: "Nowe zadanie analityczne", exact: true })
    .click();
  await page
    .getByLabel("Opis zadania", { exact: true })
    .fill("Równoległa kontrola zakończenia");
  await page
    .getByLabel("Instrukcje analizy", { exact: true })
    .fill("Weryfikacja przyjęcia aktualnego ukończonego zadania");
  await page
    .getByLabel("Wymagane przed rozwiązaniem problemu", { exact: true })
    .uncheck();
  await page
    .getByRole("button", { name: "Utwórz zadanie analityczne", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Otwórz zadanie", exact: true })
    .click();
  await page
    .getByLabel("Stan zadania analitycznego", { exact: true })
    .selectOption("IN_PROGRESS");
  await page
    .getByLabel("Wynik zadania", { exact: true })
    .fill("Moja zachowana propozycja wyniku");
  const child = (
    await (await page.request.get(`/api/v1/problems/${id}/tasks`)).json()
  ).items.find(
    (t: { title: string }) => t.title === "Równoległa kontrola zakończenia",
  );
  for (const state of ["IN_PROGRESS", "COMPLETED"]) {
    const parent = await (
      await page.request.get(`/api/v1/problems/${id}`)
    ).json();
    const current = await (
      await page.request.get(`/api/v1/problems/${id}/tasks/${child.id}`)
    ).json();
    const response = await page.request.put(
      `/api/v1/problems/${id}/tasks/${child.id}`,
      {
        headers: keyed(),
        data: {
          ...Object.fromEntries(
            [
              "title",
              "instructions",
              "required",
              "assigned_to",
              "assignment_group_id",
            ].map((k) => [k, current[k]]),
          ),
          version: current.version,
          problem_version: parent.version,
          state,
          result:
            state === "COMPLETED"
              ? "Wynik zapisany przez drugiego analityka"
              : "",
        },
      },
    );
    expect(response.status()).toBe(200);
  }
  await page
    .getByRole("button", { name: "Zapisz zadanie", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Konflikt wersji zadania", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("textbox", { name: "Wynik zadania", exact: true }),
  ).toHaveValue("Moja zachowana propozycja wyniku");
  await page
    .getByRole("button", { name: "Użyj aktualnego zadania", exact: true })
    .click();
  await expect(
    page.getByLabel("Stan zadania analitycznego", { exact: true }),
  ).toHaveValue("COMPLETED");
  await expect(
    page.getByLabel("Stan zadania analitycznego", { exact: true }),
  ).toBeDisabled();
  await expect(
    page.getByRole("textbox", { name: "Wynik zadania", exact: true }),
  ).toHaveValue("Wynik zapisany przez drugiego analityka");
  await expect(
    page.getByRole("button", { name: "Zapisz zadanie", exact: true }),
  ).toHaveCount(0);
  let discardPrompt = false;
  const onDialog = async (dialog: import("@playwright/test").Dialog) => {
    discardPrompt = true;
    await dialog.dismiss();
  };
  page.on("dialog", onDialog);
  await page
    .getByRole("button", { name: "Zamknij formularz zadania", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: child.number, exact: true }),
  ).toHaveCount(0);
  expect(discardPrompt).toBe(false);
  page.off("dialog", onDialog);
  await page.getByRole("tab", { name: "Szczegóły", exact: true }).click();
  await page
    .getByLabel("Wynik weryfikacji", { exact: true })
    .fill("Brak regresji po obciążeniu");
  await page
    .getByRole("button", { name: "Zapisz problem", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Zapisz problem", exact: true }),
  ).toBeDisabled();
  await transition("RESOLVED", "Rozwiązany");
  await transition("CLOSED", "Zamknięty");
  await page.reload();
  await expect(page.locator(".record-facts")).toContainText("Zamknięty");
  expect(
    (await (await page.request.get(`/api/v1/incidents/${inc.id}`)).json())
      .state,
  ).toBe("NEW");
  const widths = info.project.name === "chromium" ? [1366, 1920] : [412];
  for (const width of widths) {
    await page.setViewportSize({ width, height: 900 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.evaluate(() => {
      (document.activeElement as HTMLElement)?.blur();
      window.scrollTo(0, 0);
    });
    await page.screenshot({
      path: info.outputPath(`problem-${width}.png`),
      fullPage: info.project.name !== "mobile-chromium",
    });
  }
  await page.getByRole("tab", { name: "Historia", exact: true }).click();
  await expect
    .poll(
      async () =>
        (
          await (
            await page.request.get(`/api/v1/problems/${id}/history`)
          ).json()
        ).entries.length,
    )
    .toBeGreaterThan(8);
  await page
    .getByRole("button", { name: "Odśwież historię", exact: true })
    .click();
  await expect(page.locator(".timeline")).toContainText("Notatka wewnętrzna");
  const second = await page.request.post("/api/v1/problems/", {
    headers: keyed(),
    data: {
      title: `${name} extra`,
      description: "Oddzielny problem",
      impact: "LOW",
    },
  });
  expect(second.status()).toBe(200);
  await page.goto(`/problems?q=${encodeURIComponent(name)}&limit=1`);
  await expect(
    page.getByRole("region", { name: "Konfiguracja listy" }),
  ).toContainText("Dostępnych rekordów: 2");
  await page
    .getByRole("button", { name: "Następna strona", exact: true })
    .click();
  await expect(page.locator(".record-table")).toContainText(name);
  await page.goto(`/incidents/${inc.id}?tab=problems`);
  await expect(
    page.getByRole("link", { name: new RegExp(name) }).first(),
  ).toBeVisible();
});
