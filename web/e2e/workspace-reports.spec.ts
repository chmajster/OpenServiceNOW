import { test, expect, type APIRequestContext } from "@playwright/test";
import {
  loginAPI,
  permanentRootPassword,
  changeTemporaryPassword,
  personalPassword,
} from "./account-fixtures";
async function headers(r: APIRequestContext) {
  return {
    "X-CSRF-Token": (await r.storageState()).cookies.find(
      (c) => c.name === "opensnow_csrf",
    )!.value,
    "Idempotency-Key": crypto.randomUUID(),
  };
}
test("authorized global references, task URLs, context and real report drilldowns", async ({
  page,
  browser,
  request,
}, info) => {
  test.setTimeout(180000);
  const captureFullPage = info.project.name !== "mobile-chromium";
  page.setDefaultTimeout(12000);
  const tenant = process.env.E2E_TENANT ?? "e2e",
    suffix = `${info.project.name}-${Date.now()}`,
    temporary = "reports-temporary-password";
  expect(
    (
      await loginAPI(request, {
        tenant,
        email: process.env.E2E_ADMIN_EMAIL ?? "admin",
        password: permanentRootPassword,
      })
    ).status(),
  ).toBe(200);
  const adminHeaders = await headers(request);
  const user = await request.post("/api/v1/admin/users/", {
    headers: { ...adminHeaders, "Idempotency-Key": crypto.randomUUID() },
    data: {
      email: `reports-${suffix}@example.test`,
      display_name: `Raporty ${suffix}`,
      password: temporary,
    },
  });
  expect(user.status()).toBe(201);
  const uid = (await user.json()).id as string;
  const groupResponse = await request.post("/api/v1/admin/rbac/groups", {
    headers: adminHeaders,
    data: { name: `Raporty grupa ${suffix}` },
  });
  expect(groupResponse.status()).toBe(201);
  const gid = (await groupResponse.json()).id as string;
  expect(
    (
      await request.post(`/api/v1/admin/rbac/groups/${gid}/members/${uid}`, {
        headers: adminHeaders,
      })
    ).status(),
  ).toBe(204);
  const permissions = [
    "problem.read_own",
    "problem.create",
    "problem.manage",
    "problem.assign",
    "problem.tasks",
    "problem.notes",
    "change.read_own",
    "change.create",
    "change.manage",
    "change.assign",
    "change.tasks",
    "cmdb.read_own",
  ];
  const role = await request.post("/api/v1/admin/rbac/roles", {
    headers: { ...adminHeaders, "Idempotency-Key": crypto.randomUUID() },
    data: { name: `Raporty ${suffix}`, permissions },
  });
  expect(role.status()).toBe(201);
  expect(
    (
      await request.post(
        `/api/v1/admin/rbac/users/${uid}/roles/${(await role.json()).id}`,
        {
          headers: { ...adminHeaders, "Idempotency-Key": crypto.randomUUID() },
        },
      )
    ).status(),
  ).toBe(204);
  const context = await browser.newContext({
      baseURL: process.env.E2E_BASE_URL,
      viewport: page.viewportSize() ?? undefined,
      isMobile: info.project.name === "mobile-chromium",
      hasTouch: info.project.name === "mobile-chromium",
    }),
    p = await context.newPage();
  p.setDefaultTimeout(12000);
  expect(
    (
      await loginAPI(context.request, {
        tenant,
        email: `reports-${suffix}@example.test`,
        password: temporary,
      })
    ).status(),
  ).toBe(200);
  await p.goto("/account");
  await changeTemporaryPassword(p, temporary, personalPassword(temporary));
  const opHeaders = await headers(context.request);
  const problems = [];
  for (let i = 0; i < 31; i++) {
    const r = await context.request.post("/api/v1/problems/", {
      headers: { ...opHeaders, "Idempotency-Key": crypto.randomUUID() },
      data: {
        title: `Raport ${i} ${suffix}`,
        description: "Publiczne objawy",
        assigned_to: uid,
        impact: "MEDIUM",
      },
    });
    expect(r.status()).toBe(200);
    problems.push(await r.json());
  }
  const problem = problems[0];
  if (process.env.E2E_ASSERT_BIGINT)
    expect(BigInt(problem.id)).toBeGreaterThan(9007199254740991n);
  const taskResponse = await context.request.post(
    `/api/v1/problems/${problem.id}/tasks`,
    {
      headers: opHeaders,
      data: {
        problem_version: problem.version,
        assignment_group_id: gid,
        assigned_to: uid,
        title: `Analiza raportu ${suffix}`,
        instructions: "Sprawdź rzeczywiste źródła",
        required: true,
        state: "OPEN",
      },
    },
  );
  expect(taskResponse.status()).toBe(200);
  const task = await taskResponse.json();
  const ciResponse = await request.post("/api/v1/cmdb/cis", {
    headers: { ...adminHeaders, "Idempotency-Key": crypto.randomUUID() },
    data: {
      identifier: `svc-${suffix}`,
      name: `Usługa raportowa ${suffix}`,
      class: "SERVICE",
      status: "ACTIVE",
      owner_id: uid,
      environment: "PROD",
      attributes: {},
    },
  });
  expect(ciResponse.status()).toBe(200);
  const ci = await ciResponse.json();
  const current = await (
    await request.get(`/api/v1/problems/${problem.id}`)
  ).json();
  expect(
    (
      await request.put(
        `/api/v1/cmdb/records/problems/${problem.id}/cis/${ci.id}`,
        {
          headers: { ...adminHeaders, "Idempotency-Key": crypto.randomUUID() },
          data: { version: current.version, ci_version: ci.version },
        },
      )
    ).status(),
  ).toBe(200);
  const changeResponse = await context.request.post("/api/v1/changes/", {
    headers: { ...opHeaders, "Idempotency-Key": crypto.randomUUID() },
    data: {
      title: `Zmiana raportowa ${suffix}`,
      description: "Publiczny plan",
      change_type: "NORMAL",
      impact: "MEDIUM",
      risk: "LOW",
      time_zone: "Europe/Warsaw",
      source_problem_id: problem.id,
      source_version: (
        await (
          await context.request.get(`/api/v1/problems/${problem.id}`)
        ).json()
      ).version,
    },
  });
  expect(changeResponse.status()).toBe(200);
  const change = await changeResponse.json();
  await p.goto("/");
  await expect(
    p.getByRole("link", { name: "Moje aktywne problemy: 31", exact: true }),
  ).toBeVisible();
  await p
    .getByRole("link", { name: "Moje aktywne problemy: 31", exact: true })
    .click();
  await expect(p.getByLabel("Kolejka", { exact: true })).toHaveValue("mine");
  await expect(
    p.getByRole("region", { name: "Konfiguracja listy" }),
  ).toContainText("Dostępnych rekordów: 31");
  await p.goto("/reports?domain=problem");
  await expect(
    p.getByRole("heading", { name: "Raporty operacyjne", exact: true }),
  ).toBeVisible();
  const stateReport = p.getByRole("article").filter({
    has: p.getByRole("heading", {
      name: "Problemy — aktywne według stanu",
      exact: true,
    }),
  });
  await expect(
    stateReport.getByRole("link", { name: "NEW: 31", exact: true }),
  ).toBeVisible();
  await stateReport.getByRole("link", { name: "NEW: 31", exact: true }).click();
  await expect(
    p.getByRole("status").filter({ hasText: "Dostępnych rekordów: 31" }),
  ).toBeVisible();
  await expect(p.locator(".record-table tbody tr")).toHaveCount(25);
  await p.getByRole("button", { name: "Następna strona", exact: true }).click();
  await expect(p.locator(".record-table tbody tr")).toHaveCount(6);
  await p.reload();
  await expect(p.locator(".record-table tbody tr")).toHaveCount(6);
  await p.getByRole("button", { name: "Pierwsza strona", exact: true }).click();
  await expect(p.locator(".record-table tbody tr")).toHaveCount(25);
  await p.getByLabel("Wyszukaj rekordy", { exact: true }).fill(task.number);
  await p.getByRole("button", { name: "Szukaj", exact: true }).click();
  const result = p.getByRole("region", { name: "Wyniki wyszukiwania" });
  await expect(result).toContainText("Zadanie analityczne");
  await result.getByRole("link").first().click();
  await expect(p).toHaveURL(new RegExp(`task_id=${task.id}`));
  await expect(
    p.getByRole("heading", { name: task.number, exact: true }),
  ).toBeVisible();
  await p.reload();
  await expect(
    p.getByRole("heading", { name: task.number, exact: true }),
  ).toBeVisible();
  await expect(p.getByLabel("Opis zadania", { exact: true })).toBeDisabled();
  await expect(
    p.getByRole("button", { name: "Zapisz zadanie", exact: true }),
  ).toHaveCount(0);
  await expect(
    p.getByRole("button", { name: "Edytuj wskazane zadanie", exact: true }),
  ).toBeVisible();
  await p.getByRole("button", { name: "Kontekst", exact: true }).click();
  const panel = p.locator(".context-panel");
  await expect(
    panel.getByRole("link", { name: new RegExp(ci.name) }),
  ).toBeVisible();
  await expect(
    panel.getByRole("link", { name: new RegExp(change.number) }),
  ).toBeVisible();
  await expect(panel).toContainText("Zadania");
  await p.getByLabel("Wyszukaj rekordy", { exact: true }).fill(ci.identifier);
  await p.getByRole("button", { name: "Szukaj", exact: true }).click();
  await expect(result).toContainText("Usługa biznesowa");
  await expect(result).toContainText("Dokładny identyfikator");
  await p.keyboard.press("Escape");
  await expect(result).toHaveCount(0);
  await p.goto(`/problems/${problem.id}`);
  await p.getByText("Podgląd: Przypisany analityk", { exact: true }).click();
  await expect(p.locator(".reference-choice-preview")).toContainText(
    `Raporty ${suffix}`,
  );
  const query = await context.request.get(`/api/v1/workspace/search?q=Raport`);
  expect(query.status()).toBe(200);
  const found = (await query.json()).items;
  expect(
    found.filter((v: { type: string }) => v.type === "problem"),
  ).toHaveLength(10);
  await p.goto("/reports?domain=change");
  await expect(
    p.getByRole("heading", {
      name: "Zmiany — aktywne według stanu",
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    p.getByText("Zmiany — zagrożone SLA", { exact: true }),
  ).toHaveCount(0);
  if (info.project.name === "chromium") {
    for (const width of [1366, 1920]) {
      await p.setViewportSize({ width, height: 900 });
      await p.screenshot({
        path: info.outputPath(`reports-${width}.png`),
        fullPage: captureFullPage,
      });
    }
  } else {
    expect(
      await p.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await p.screenshot({
      path: info.outputPath("reports-mobile.png"),
      fullPage: captureFullPage,
    });
  }
  await context.close();
});
