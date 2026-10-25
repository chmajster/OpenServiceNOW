import { test, expect, type Page, type Locator } from "@playwright/test";

// This suite deliberately uses a running local API/MySQL/worker. It contains
// no route interception or runtime fixtures masquerading as backend results.
test.skip(
  process.env.E2E_MODULE08_REAL !== "1",
  "Requires the isolated local Module 08 runtime",
);
test.use({
  launchOptions: { executablePath: process.env.E2E_CHROMIUM_EXECUTABLE },
});

async function login(page: Page) {
  await page.goto("/login");
  await page
    .getByLabel("Organizacja", { exact: true })
    .fill(process.env.E2E_TENANT!);
  await page
    .getByLabel("Login lub e-mail", { exact: true })
    .fill(process.env.E2E_EMAIL!);
  await page
    .getByLabel("Hasło", { exact: true })
    .fill(process.env.E2E_PASSWORD!);
  await page.getByRole("button", { name: "Zaloguj", exact: true }).click();
  await page.waitForURL("/");
}
async function api(
  page: Page,
  path: string,
  method = "GET",
  body?: unknown,
  csrf = true,
) {
  const result = await page.evaluate(
    async ({ path, method, body, csrf }) => {
      const token =
        document.cookie
          .split("; ")
          .find((c) => c.startsWith("opensnow_csrf="))
          ?.split("=")[1] ?? "";
      const response = await fetch(path, {
        method,
        headers: {
          "Content-Type": "application/json",
          ...(csrf ? { "X-CSRF-Token": decodeURIComponent(token) } : {}),
          "Idempotency-Key": crypto.randomUUID(),
        },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      return { status: response.status, body: await response.json() };
    },
    { path, method, body, csrf },
  );
  return result;
}
async function submit(
  page: Page,
  button: Locator,
  path: string,
  method: string,
  status = 200,
) {
  const [response] = await Promise.all([
    page.waitForResponse(
      (r) => r.url().endsWith(path) && r.request().method() === method,
    ),
    button.click(),
  ]);
  expect(response.status(), await response.text()).toBe(status);
}
async function choose(page: Page, scope: Locator, label: string) {
  await scope.getByRole("combobox", { name: label, exact: true }).click();
  await page
    .getByRole("option", { name: new RegExp(process.env.E2E_DISPLAY_NAME!) })
    .first()
    .click();
}
async function localDate(page: Page, milliseconds: number) {
  return page.evaluate((value) => {
    const parts = new Intl.DateTimeFormat("en-CA", {
      timeZone: document.documentElement.dataset.timeZone || "UTC",
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      hourCycle: "h23",
    }).formatToParts(new Date(value));
    const get = (key: string) => parts.find((p) => p.type === key)!.value;
    return `${get("year")}-${get("month")}-${get("day")}T${get("hour")}:${get("minute")}`;
  }, milliseconds);
}

test("real incident → coordination → publication → workspace task → restoration → review", async ({
  page,
  context,
}, testInfo) => {
  test.setTimeout(120_000);
  const pageErrors: string[] = [];
  page.on("pageerror", (e) => pageErrors.push(e.message));
  await login(page);
  const suffix = `${testInfo.project.name}-${Date.now()}`;
  await page.goto("/incidents/new");
  await page
    .getByLabel("Tytuł", { exact: true })
    .fill(`Real browser outage ${suffix}`);
  await page
    .getByLabel("Opis", { exact: true })
    .fill("Controlled local service incident, no external integration.");
  await page
    .getByRole("button", { name: "Utwórz incydent", exact: true })
    .click();
  await page.getByRole("link", { name: /Otwórz INC/ }).click();
  const id = page.url().split("/").at(-1)!;
  const base = `/api/v1/incidents/${id}/major`;
  await page.goto(`/incidents/${id}?tab=major`);
  await page
    .getByRole("textbox", { name: "Uzasadnienie decyzji", exact: true })
    .fill("Confirmed impact requires coordination");
  await submit(
    page,
    page.getByRole("button", { name: "Zgłoś kandydata", exact: true }),
    `${base}/decisions/nominate`,
    "POST",
  );
  await page
    .getByRole("textbox", { name: "Uzasadnienie decyzji", exact: true })
    .fill("Approved by the local coordinator");
  await submit(
    page,
    page.getByRole("button", { name: "Promuj do dużej awarii", exact: true }),
    `${base}/decisions/promote`,
    "POST",
  );
  const coordination = page.locator("form").filter({
    has: page.getByRole("combobox", {
      name: "Incident commander",
      exact: true,
    }),
  });
  await choose(page, coordination, "Incident commander");
  await choose(page, coordination, "Właściciel komunikacji");
  await coordination
    .getByRole("textbox", { name: "Podsumowanie wpływu", exact: true })
    .fill("Confirmed local test impact");
  await coordination
    .getByRole("textbox", { name: "Aktualne obejście", exact: true })
    .fill("Manual failover");
  await coordination
    .getByLabel(/Rzeczywisty czas wykrycia/)
    .fill(await localDate(page, Date.now() - 3600_000));
  await coordination
    .getByLabel("Adres mostka konferencyjnego (HTTPS)", { exact: true })
    .fill("https://example.test/controlled-bridge");
  await submit(
    page,
    coordination.getByRole("button", { name: "Zapisz", exact: true }),
    `${base}/coordination`,
    "PUT",
  );

  const publish = page.getByRole("form", {
    name: "Opublikuj komunikat",
    exact: true,
  });
  await publish
    .getByRole("textbox", { name: "Treść komunikatu", exact: true })
    .fill(`Public update ${suffix}`);
  // Actual network loss preserves the exact local draft and retry key.
  await context.setOffline(true);
  await publish
    .getByRole("button", { name: "Opublikuj komunikat", exact: true })
    .click();
  await expect(
    page.getByRole("alert").filter({ hasText: /./ }).first(),
  ).toBeVisible();
  await expect(
    page.getByRole("textbox", { name: "Treść komunikatu", exact: true }),
  ).toHaveValue(`Public update ${suffix}`);
  await context.setOffline(false);
  await submit(
    page,
    publish.getByRole("button", { name: "Opublikuj komunikat", exact: true }),
    `${base}/updates`,
    "POST",
    201,
  );
  await expect(
    publish.getByRole("textbox", { name: "Treść komunikatu", exact: true }),
  ).toHaveValue("");
  await publish
    .getByRole("combobox", {
      name: "Odbiorcy publicznych komunikatów",
      exact: true,
    })
    .selectOption("OPERATORS");
  await publish
    .getByRole("textbox", { name: "Treść komunikatu", exact: true })
    .fill(`Private update ${suffix}`);
  await submit(
    page,
    publish.getByRole("button", { name: "Opublikuj komunikat", exact: true }),
    `${base}/updates`,
    "POST",
    201,
  );
  await expect(
    publish.getByRole("textbox", { name: "Treść komunikatu", exact: true }),
  ).toHaveValue("");
  await expect(
    page.getByText(`Private update ${suffix}`, { exact: true }),
  ).toBeVisible();

  const task = page.locator("form").filter({
    has: page.getByRole("heading", {
      name: "Dodaj zadanie koordynacyjne",
      exact: true,
    }),
  });
  await task
    .getByRole("textbox", { name: "Opis działania", exact: true })
    .fill(`Validate failover ${suffix}`);
  await choose(page, task, "Właściciel");
  await task
    .getByLabel(/Termin działania/)
    .fill(await localDate(page, Date.now() + 86400_000));
  await submit(
    page,
    task.getByRole("button", {
      name: "Dodaj zadanie koordynacyjne",
      exact: true,
    }),
    `${base}/actions`,
    "POST",
    201,
  );
  await expect(
    task.getByRole("textbox", { name: "Opis działania", exact: true }),
  ).toHaveValue("");
  const actionLink = page.getByRole("link", { name: /^ACT/ }).first();
  await actionLink.click();
  await page
    .getByRole("textbox", { name: "Notatki zadania", exact: true })
    .fill(`Durable private task note ${suffix}`);
  const actionID = new URL(page.url()).searchParams.get("action_id")!;
  await submit(
    page,
    page.getByRole("button", { name: "Dodaj notatkę", exact: true }),
    `/api/v1/incidents/major/actions/${actionID}/messages`,
    "POST",
    201,
  );
  await page.reload();
  await expect(
    page.getByText(`Durable private task note ${suffix}`, { exact: true }),
  ).toBeVisible();

  // Exercise real API authorization and the shared incident state machine.
  const current = (await api(page, `/api/v1/incidents/${id}`)).body;
  const self = (
    await api(
      page,
      "/api/v1/incidents/lookups/users?purpose=coordination&limit=25",
    )
  ).body.users.find(
    (u: { display_name: string }) =>
      u.display_name === process.env.E2E_DISPLAY_NAME,
  );
  let record = (
    await api(page, `/api/v1/incidents/${id}/assignment`, "PUT", {
      version: current.version,
      assigned_to: self.id,
      assignment_group_id: null,
      reason: "Test coordinator owns restoration",
    })
  ).body;
  record = (
    await api(page, `/api/v1/incidents/${id}/transition`, "POST", {
      version: record.version,
      state: "IN_PROGRESS",
    })
  ).body;
  const denied = await api(
    page,
    `/api/v1/incidents/${id}/transition`,
    "POST",
    {
      version: record.version,
      state: "RESOLVED",
      resolution_code: "FIXED",
      resolution_description: "Verified failover",
    },
    false,
  );
  expect(denied.status).toBe(403);
  const resolved = await api(
    page,
    `/api/v1/incidents/${id}/transition`,
    "POST",
    {
      version: record.version,
      state: "RESOLVED",
      resolution_code: "FIXED",
      resolution_description: "Verified failover",
    },
  );
  expect(resolved.status).toBe(200);
  await page.goto(`/incidents/${id}?tab=major`);
  const restoration = page.locator("form").filter({
    has: page.getByRole("heading", {
      name: "Potwierdź przywrócenie",
      exact: true,
    }),
  });
  await restoration
    .getByLabel(/Rzeczywisty czas przywrócenia/)
    .fill(await localDate(page, Date.now() - 60_000));
  await restoration
    .getByRole("textbox", { name: "Uzasadnienie decyzji", exact: true })
    .fill("Verified service recovery");
  await submit(
    page,
    restoration.getByRole("button", {
      name: "Potwierdź przywrócenie",
      exact: true,
    }),
    `${base}/restoration`,
    "POST",
  );
  const review = page.locator("form").filter({
    has: page.getByRole("heading", {
      name: "Przegląd po awarii",
      exact: true,
    }),
  });
  await review
    .getByRole("textbox", { name: "Podsumowanie wpływu", exact: true })
    .fill("Confirmed impact recorded");
  await review
    .getByRole("textbox", {
      name: "Przyczyny znane na ten moment",
      exact: true,
    })
    .fill("Controlled test failure");
  await review
    .getByRole("textbox", { name: "Co zadziałało", exact: true })
    .fill("Manual failover and clear communication");
  await submit(
    page,
    review.getByRole("button", { name: "Zakończ przegląd", exact: true }),
    `${base}/review`,
    "PUT",
  );
  await expect(page.getByText(/Przegląd zakończony:/)).toBeVisible();
  const close = await api(page, `/api/v1/incidents/${id}/transition`, "POST", {
    version: resolved.body.version,
    state: "CLOSED",
  });
  expect(close.status).toBe(200);
  await page.goto(`/workspace/major?view=actions&action_id=${actionID}`);
  await expect(
    page.getByText(`Validate failover ${suffix}`, { exact: true }).first(),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Rozpocznij", exact: true }).first(),
  ).toBeEnabled();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1,
    ),
  ).toBe(true);
  const header = await page.evaluate(() => {
    const brand = document
        .querySelector(".topbar-brand")!
        .getBoundingClientRect(),
      search = document
        .querySelector(".topbar-global-search")!
        .getBoundingClientRect(),
      actions = document
        .querySelector(".topbar-actions")!
        .getBoundingClientRect();
    return {
      brandRight: brand.right,
      searchLeft: search.left,
      searchRight: search.right,
      actionsLeft: actions.left,
    };
  });
  expect(header.brandRight).toBeLessThanOrEqual(header.searchLeft + 1);
  expect(header.searchRight).toBeLessThanOrEqual(header.actionsLeft + 1);
  expect(pageErrors).toEqual([]);
  await page.screenshot({
    path: testInfo.outputPath("coordination-actions.png"),
    fullPage: true,
  });
});

test("real configuration publishes tenant policy and reconciles a stale draft", async ({
  page,
}) => {
  await login(page);
  await page.goto("/incidents/admin");
  const hours = page.getByLabel(
    "Automatyczne zamknięcie po rozwiązaniu (godziny, 0 — wyłączone)",
    { exact: true },
  );
  await hours.fill("");
  await hours.fill("12");
  const head = (await api(page, "/api/v1/metadata/configuration")).body;
  const competing = await api(
    page,
    "/api/v1/metadata/configuration/draft",
    "PUT",
    {
      version: head.version,
      configuration: {
        ...head.draft,
        incident_process: {
          ...head.draft.incident_process,
          auto_close_hours: 24,
        },
      },
    },
  );
  expect(competing.status).toBe(200);
  await submit(
    page,
    page.getByRole("button", { name: "Zapisz wersję roboczą", exact: true }),
    "/api/v1/metadata/configuration/draft",
    "PUT",
    409,
  );
  await expect(hours).toHaveValue("12");
  await page
    .getByRole("button", {
      name: "Porównano dane — zachowaj szkic z aktualną wersją",
      exact: true,
    })
    .click();
  await submit(
    page,
    page.getByRole("button", { name: "Zapisz wersję roboczą", exact: true }),
    "/api/v1/metadata/configuration/draft",
    "PUT",
  );
  await submit(
    page,
    page.getByRole("button", {
      name: "Opublikuj zapisaną konfigurację",
      exact: true,
    }),
    "/api/v1/metadata/configuration/publish",
    "POST",
  );
  await page.reload();
  await expect(hours).toHaveValue("12");
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1,
    ),
  ).toBe(true);
});
