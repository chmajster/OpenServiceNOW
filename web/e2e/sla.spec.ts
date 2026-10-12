import { test, expect, type APIRequestContext } from "@playwright/test";
import {
  loginAPI,
  permanentRootPassword,
  changeTemporaryPassword,
  personalPassword,
} from "./account-fixtures";

async function csrf(request: APIRequestContext) {
  return {
    "X-CSRF-Token": (await request.storageState()).cookies.find(
      (c) => c.name === "opensnow_csrf",
    )!.value,
  };
}
test("SLA administration, explicit conflict, first reply and autonomous breach survive reload", async ({
  page,
  browser,
  request,
}, info) => {
  test.setTimeout(240000);
  const tenant = process.env.E2E_TENANT ?? "e2e",
    scope = `sla-${info.project.name}-${Date.now()}`;
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
  await page.context().addCookies((await request.storageState()).cookies);
  const me = await (await request.get("/api/v1/auth/me")).json();
  const userResponse = await request.post("/api/v1/admin/users/", {
    headers,
    data: {
      email: `${scope}@example.test`,
      display_name: scope,
      password: "sla-temporary-password",
    },
  });
  expect(userResponse.status()).toBe(201);
  const user = (await userResponse.json()).id;
  const roleResponse = await request.post("/api/v1/admin/rbac/roles", {
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
  expect(roleResponse.status()).toBe(201);
  const role = (await roleResponse.json()).id;
  expect(
    (
      await request.post(`/api/v1/admin/rbac/users/${user}/roles/${role}`, {
        headers,
      })
    ).status(),
  ).toBe(204);
  const reporterContext = await browser.newContext({
    baseURL: process.env.E2E_BASE_URL ?? "http://127.0.0.1:5173",
  });
  const reporter = await reporterContext.newPage();
  let policyID: string | undefined;
  try {
    await page.goto("/sla?kind=calendar");
    await page
      .getByRole("link", { name: "Nowy kalendarz", exact: true })
      .click();
    await page.getByLabel("Nazwa", { exact: true }).fill(`Kalendarz ${scope}`);
    for (const day of [
      "Niedziela",
      "Poniedziałek",
      "Wtorek",
      "Środa",
      "Czwartek",
      "Piątek",
      "Sobota",
    ])
      await page.getByLabel(day, { exact: true }).fill("00:00-24:00");
    await page
      .getByLabel("Święta — lokalne daty, po jednej w wierszu")
      .fill("2035-12-25");
    await page
      .getByLabel("Wyjątki — data=godziny, po jednym w wierszu")
      .fill("2035-12-24=09:00-12:00");
    const calSaved = page.waitForResponse(
      (r) =>
        r.url().includes("/sla/definitions/calendar") &&
        r.request().method() === "POST",
    );
    await page.getByRole("button", { name: "Opublikuj wersję" }).click();
    const calendar = await (await calSaved).json();
    expect(calendar.version).toBe("1");
    await expect(page.getByRole("status")).toContainText("Zapisano wersję");
    await page.getByRole("link", { name: "Polityki SLA", exact: true }).click();
    await page
      .getByRole("link", { name: "Nowa polityka SLA", exact: true })
      .click();
    await page.getByLabel("Nazwa", { exact: true }).fill(`Polityka ${scope}`);
    await page.getByLabel("Kolejność wyboru").fill("1");
    await page.getByLabel("Priorytet", { exact: true }).selectOption("5");
    await page.getByLabel("Kategoria", { exact: true }).selectOption("other");
    await page.getByLabel("Wersja kalendarza").selectOption(`${calendar.id}:1`);
    await page.getByLabel("Pierwsza odpowiedź — minuty robocze").fill("5");
    await page.getByLabel("Rozwiązanie — minuty robocze").fill("1");
    await page.getByLabel("Ostrzeżenie — procent celu").fill("50");
    await page
      .getByLabel(
        "Pauzujące powody wstrzymania — dokładna treść, po jednym w wierszu",
      )
      .fill("Klient");
    const policySaved = page.waitForResponse(
      (r) =>
        r.url().includes("/sla/definitions/policy") &&
        r.request().method() === "POST",
    );
    await page.getByRole("button", { name: "Opublikuj wersję" }).click();
    let policy = await (await policySaved).json();
    policyID = policy.id;
    await expect(page.getByRole("status")).toContainText("Zapisano wersję");
    // A real competing API edit must not erase the local form.
    const localName = `Lokalna ${scope}`;
    await page.getByLabel("Nazwa", { exact: true }).fill(localName);
    expect(
      (
        await request.put(`/api/v1/sla/definitions/policy/${policy.id}`, {
          headers,
          data: {
            version: policy.version,
            config: { ...policy.config, name: `Zdalna ${scope}` },
          },
        })
      ).status(),
    ).toBe(200);
    await page.getByRole("button", { name: "Opublikuj wersję" }).click();
    await expect(
      page.getByRole("region", { name: "Konflikt wersji" }),
    ).toBeVisible();
    await expect(page.getByLabel("Nazwa", { exact: true })).toHaveValue(
      localName,
    );
    await page
      .getByRole("button", { name: "Zachowaj propozycję do ponownego zapisu" })
      .click();
    await page.getByRole("button", { name: "Opublikuj wersję" }).click();
    await expect(page.getByRole("status")).toContainText("Zapisano wersję");
    policy = await (
      await request.get(`/api/v1/sla/definitions/policy/${policy.id}`)
    ).json();
    expect(policy.version).toBe("3");
    expect(
      (
        await loginAPI(reporter.request, {
          tenant,
          email: `${scope}@example.test`,
          password: "sla-temporary-password",
        })
      ).status(),
    ).toBe(200);
    await reporter.goto("/account");
    await changeTemporaryPassword(
      reporter,
      "sla-temporary-password",
      personalPassword("sla-temporary-password"),
    );
    const created = await reporter.request.post("/api/v1/incidents/", {
      headers: { ...(await csrf(reporter.request)), "Idempotency-Key": scope },
      data: {
        title: scope,
        description: "Asynchroniczny SLA",
        category: "other",
        subcategory: "other",
        impact: 3,
        urgency: 3,
      },
    });
    expect(created.status()).toBe(201);
    let incident = await created.json();
    const slaPath = `/api/v1/sla/incidents?ids=${incident.id}`;
    await expect
      .poll(
        async () => (await (await request.get(slaPath)).json()).items.length,
        { timeout: 15000 },
      )
      .toBe(2);
    if (process.env.E2E_BIGINT === "1")
      expect(
        BigInt((await (await request.get(slaPath)).json()).items[0].id) >
          9007199254740991n,
      ).toBe(true);
    expect(
      (
        await request.put(`/api/v1/incidents/${incident.id}/assignment`, {
          headers,
          data: {
            version: incident.version,
            assigned_to: me.user.id,
            assignment_group_id: null,
          },
        })
      ).status(),
    ).toBe(200);
    incident = await (
      await request.get(`/api/v1/incidents/${incident.id}`)
    ).json();
    expect(
      (
        await request.post(`/api/v1/incidents/${incident.id}/transition`, {
          headers,
          data: {
            version: incident.version,
            state: "ON_HOLD",
            reason: "Klient",
          },
        })
      ).status(),
    ).toBe(200);
    await expect
      .poll(
        async () =>
          (await (await request.get(slaPath)).json()).items.every(
            (i: { status: string }) => i.status === "PAUSED",
          ),
        { timeout: 15000 },
      )
      .toBe(true);
    incident = await (
      await request.get(`/api/v1/incidents/${incident.id}`)
    ).json();
    expect(
      (
        await request.post(`/api/v1/incidents/${incident.id}/transition`, {
          headers,
          data: { version: incident.version, state: "IN_PROGRESS" },
        })
      ).status(),
    ).toBe(200);
    // Change calendar configuration after enrollment; original version stays pinned.
    expect(
      (
        await request.put(`/api/v1/sla/definitions/calendar/${calendar.id}`, {
          headers,
          data: {
            version: "1",
            config: { ...calendar.config, name: `Nowy ${scope}` },
          },
        })
      ).status(),
    ).toBe(200);
    await page.goto(`/incidents/${incident.id}?tab=communication`);
    await page
      .getByLabel("Treść odpowiedzi", { exact: true })
      .fill("Operator przyjął zgłoszenie");
    await page
      .getByRole("button", { name: "Wyślij odpowiedź", exact: true })
      .click();
    await expect
      .poll(
        async () =>
          (await (await request.get(slaPath)).json()).items.find(
            (i: { kind: string }) => i.kind === "response",
          )?.status,
        { timeout: 15000 },
      )
      .toBe("COMPLETED");
    // No incident view remains open while the scheduler/worker crosses the deadline.
    await page.goto("about:blank");
    await reporter.goto("about:blank");
    await expect
      .poll(
        async () =>
          !!(await (await request.get(slaPath)).json()).items.find(
            (i: { kind: string }) => i.kind === "resolution",
          )?.breached_at,
        { timeout: 100000, intervals: [1000, 3000, 5000] },
      )
      .toBe(true);
    const final = (await (await request.get(slaPath)).json()).items;
    expect(
      final.every(
        (i: { calendar_version: string; policy_version: string }) =>
          i.calendar_version === "1" && i.policy_version === "3",
      ),
    ).toBe(true);
    await expect
      .poll(
        async () =>
          (
            await (await reporter.request.get("/api/v1/notifications/")).json()
          ).notifications.filter(
            (n: { summary: string }) =>
              n.summary === "Przekroczono termin SLA incydentu",
          ).length,
        { timeout: 15000 },
      )
      .toBe(1);
    await page.goto(`/incidents/${incident.id}`);
    await page.reload();
    await expect(
      page.getByText("Naruszenie", { exact: true }).first(),
    ).toBeVisible();
    await expect(
      page.getByText("Polityka / cykl", { exact: true }).first(),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.goto(`/incidents?q=${scope}`);
    await expect(
      page.getByRole("columnheader", { name: "SLA", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Naruszenie", { exact: false }).first(),
    ).toBeVisible();
    await reporter.goto(`/incidents/${incident.id}`);
    await expect(
      reporter.getByRole("heading", { name: "Poziom obsługi — SLA" }),
    ).toBeVisible();
    expect(
      (await reporter.request.get("/api/v1/sla/definitions/policy")).status(),
    ).toBe(403);
  } finally {
    if (policyID) {
      const current = await (
        await request.get(`/api/v1/sla/definitions/policy/${policyID}`)
      ).json();
      await request.put(`/api/v1/sla/definitions/policy/${policyID}`, {
        headers,
        data: {
          version: current.version,
          config: { ...current.config, enabled: false },
        },
      });
    }
    await reporterContext.close();
  }
});
