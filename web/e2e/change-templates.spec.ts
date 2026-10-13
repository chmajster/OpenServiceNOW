import {
  expect,
  test,
  type Page,
  type APIRequestContext,
} from "@playwright/test";
import {
  loginAPI,
  permanentRootPassword,
  changeTemporaryPassword,
  personalPassword,
} from "./account-fixtures";
const tenant = process.env.E2E_TENANT ?? "e2e";
async function headers(r: APIRequestContext) {
  return {
    "X-CSRF-Token": (await r.storageState()).cookies.find(
      (c) => c.name === "opensnow_csrf",
    )!.value,
    "Idempotency-Key": crypto.randomUUID(),
  };
}
async function choose(p: Page, label: string, name: string) {
  const input = p.getByRole("combobox", { name: label, exact: true });
  await input.fill(name);
  await expect(p.getByRole("option", { name, exact: true })).toBeVisible();
  await input.press("ArrowDown");
  await input.press("Enter");
}
test("Standard template requires independent publication and instance approval; Emergency keeps explicit authorization", async ({
  page,
  browser,
  request,
}, info) => {
  test.setTimeout(240000);
  page.setDefaultTimeout(12000);
  expect(
    (
      await loginAPI(request, {
        tenant,
        email: process.env.E2E_ADMIN_EMAIL ?? "admin",
        password: permanentRootPassword,
      })
    ).status(),
  ).toBe(200);
  const suffix = `${info.project.name}-${Date.now()}`,
    me = await (await request.get("/api/v1/auth/me")).json(),
    h = await headers(request),
    temporary = "template-approver-password",
    email = `template-approver-${suffix}@example.test`;
  const u = await request.post("/api/v1/admin/users/", {
    headers: h,
    data: { email, display_name: `CAB ${suffix}`, password: temporary },
  });
  expect(u.status()).toBe(201);
  const uid = (await u.json()).id;
  const role = await request.post("/api/v1/admin/rbac/roles", {
    headers: h,
    data: {
      name: `Template CAB ${suffix}`,
      permissions: [
        "workflow.approve",
        "change.read",
        "change.template_approve",
        "cmdb.read",
      ],
    },
  });
  expect(role.status()).toBe(201);
  expect(
    (
      await request.post(
        `/api/v1/admin/rbac/users/${uid}/roles/${(await role.json()).id}`,
        { headers: h },
      )
    ).status(),
  ).toBe(204);
  const group = await request.post("/api/v1/admin/rbac/groups", {
    headers: h,
    data: { name: `Standard ${suffix}` },
  });
  expect(group.status()).toBe(201);
  const gid = (await group.json()).id;
  expect(
    (
      await request.post(
        `/api/v1/admin/rbac/groups/${gid}/members/${me.user.id}`,
        { headers: h },
      )
    ).status(),
  ).toBe(204);
  const ciResp = await request.post("/api/v1/cmdb/cis", {
    headers: await headers(request),
    data: {
      identifier: `template-ci-${suffix}`,
      name: `CI ${suffix}`,
      class: "APPLICATION",
      environment: "PROD",
      status: "ACTIVE",
      owner_id: me.user.id,
      attributes: {},
    },
  });
  expect(ciResp.status()).toBe(200);
  const ci = await ciResp.json();
  async function definition(kind: string) {
    const name = `${kind} ${suffix}`,
      draft = {
        domain: "CHANGE",
        change_type: kind,
        name,
        executor_id: me.user.id,
        group_id: gid,
        self_approval: "DENY",
        nodes: [
          { id: "start", label: "Start", type: "START", next: "approval" },
          {
            id: "approval",
            label: "Decyzja CAB",
            type: "APPROVAL",
            target_type: "USER",
            target_id: uid,
            policy: "ALL",
            seconds: 3600,
            next: "authorize",
            rejected: "reject",
            timeout: "reject",
          },
          {
            id: "authorize",
            label: "Autoryzacja",
            type: "UPDATE_RECORD",
            state: "AUTHORIZED",
            reason: "Zaakceptowano oceniany zakres",
            next: "end",
          },
          {
            id: "reject",
            label: "Odrzucenie",
            type: "UPDATE_RECORD",
            state: "REJECTED",
            reason: "Odrzucono oceniany zakres",
            next: "end",
          },
          { id: "end", label: "Koniec", type: "END" },
        ],
      };
    const saved = await request.post("/api/v1/workflow/definitions", {
      headers: await headers(request),
      data: { version: "0", draft },
    });
    expect(saved.status()).toBe(200);
    const d = await saved.json(),
      published = await request.post(
        `/api/v1/workflow/definitions/${d.id}/publish`,
        { headers: await headers(request), data: { version: d.version } },
      );
    expect(published.status()).toBe(200);
    return name;
  }
  const context = await browser.newContext({
      baseURL: process.env.E2E_BASE_URL,
      viewport: page.viewportSize() ?? undefined,
    }),
    ap = await context.newPage();
  ap.setDefaultTimeout(12000);
  try {
    expect(
      (
        await loginAPI(context.request, { tenant, email, password: temporary })
      ).status(),
    ).toBe(200);
    await ap.goto("/account");
    await changeTemporaryPassword(ap, temporary, personalPassword(temporary));
    const standard = await definition("STANDARD"),
      emergency = await definition("EMERGENCY");
    await page.context().addCookies((await request.storageState()).cookies);
    const templateName = `Procedura ${suffix}`;
    await page.goto("/change-templates/new");
    await page.getByLabel("Nazwa szablonu", { exact: true }).fill(templateName);
    await page
      .getByLabel("Opis skrócony", { exact: true })
      .fill("Rutynowe wdrożenie konfiguracji");
    await page
      .getByLabel("Opis zmiany", { exact: true })
      .fill("Opisana procedura zmiany");
    await choose(page, "Opublikowany proces akceptacji", standard);
    for (const label of [
      "Uzasadnienie zmiany",
      "Zakres zmiany",
      "Plan wdrożenia",
      "Plan testów",
      "Plan wycofania",
    ])
      await page
        .getByLabel(label, { exact: true })
        .fill(`Zatwierdzana procedura: ${label}`);
    await choose(page, "CI szablonu", ci.name);
    await page
      .getByRole("button", { name: "Dodaj CI do szablonu", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Zapisz wersję roboczą", exact: true })
      .click();
    await expect(page).toHaveURL(/change-templates\/[0-9]+$/);
    const templateID = new URL(page.url()).pathname.split("/").at(-1)!;
    if (process.env.E2E_ASSERT_BIGINT)
      expect(BigInt(templateID)).toBeGreaterThan(9007199254740991n);
    await expect(
      page.getByRole("button", {
        name: "Zatwierdź tę wersję szablonu",
        exact: true,
      }),
    ).toBeDisabled();
    await ap.goto(`/change-templates/${templateID}`);
    await ap
      .getByRole("button", {
        name: "Zatwierdź tę wersję szablonu",
        exact: true,
      })
      .click();
    await expect(ap.locator("main")).toContainText("Zatwierdzona wersja: 1");
    const date = new Date(Date.now() + 3600000),
      end = new Date(date.getTime() + 1800000),
      wall = (d: Date) =>
        new Intl.DateTimeFormat("en-CA", {
          timeZone: "Europe/Warsaw",
          year: "numeric",
          month: "2-digit",
          day: "2-digit",
          hour: "2-digit",
          minute: "2-digit",
          hourCycle: "h23",
        })
          .format(d)
          .replace(", ", "T");
    await page.goto("/changes/new");
    await page
      .getByRole("combobox", { name: "Typ zmiany", exact: true })
      .selectOption("STANDARD");
    await page
      .getByRole("combobox", { name: "Wersja szablonu", exact: true })
      .selectOption({ label: `${templateName} · wersja 1` });
    await expect(
      page.getByRole("textbox", { name: "Zakres zmiany", exact: true }),
    ).toHaveValue("Zatwierdzana procedura: Zakres zmiany");
    await expect(
      page.getByRole("link", { name: new RegExp(ci.name) }),
    ).toBeVisible();
    await page
      .getByLabel("Planowany początek", { exact: true })
      .fill(wall(date));
    await page.getByLabel("Planowany koniec", { exact: true }).fill(wall(end));
    await page
      .getByRole("button", { name: "Utwórz zmianę", exact: true })
      .click();
    await expect(page).toHaveURL(/\/changes\/[0-9]+$/);
    const changeID = new URL(page.url()).pathname.split("/").at(-1)!;
    await page
      .getByRole("tab", { name: "Powiązane rekordy", exact: true })
      .click();
    await choose(page, "Element konfiguracji", ci.name);
    await page.getByRole("button", { name: "Powiąż CI", exact: true }).click();
    await expect(
      page.getByRole("status").filter({ hasText: "Zapisano powiązanie CI." }),
    ).toBeVisible();
    await page.getByRole("tab", { name: "Szczegóły", exact: true }).click();
    await page
      .getByLabel("Przejście procesu", { exact: true })
      .selectOption("AUTHORIZE");
    await page
      .getByRole("button", { name: "Wykonaj przejście", exact: true })
      .click();
    await expect(page.locator(".record-facts")).toContainText("Autoryzacja");
    let c = await (await request.get(`/api/v1/changes/${changeID}`)).json();
    expect(c.approval_state).toBe("PENDING");
    await expect
      .poll(
        async () =>
          (
            await (
              await request.get(`/api/v1/workflow/executions/${c.execution_id}`)
            ).json()
          ).state,
        { timeout: 30000 },
      )
      .toBe("WAITING");
    await ap.goto(`/approvals/${c.execution_id}`);
    await ap
      .getByLabel("Uzasadnienie", { exact: true })
      .fill("Standard zgodny z zatwierdzonym szablonem");
    await ap
      .getByRole("button", { name: "Zapisz decyzję", exact: true })
      .click();
    await expect
      .poll(
        async () =>
          (await (await request.get(`/api/v1/changes/${changeID}`)).json())
            .approval_state,
        { timeout: 30000 },
      )
      .toBe("APPROVED");
    await page.goto("/changes/new");
    await page
      .getByRole("combobox", { name: "Typ zmiany", exact: true })
      .selectOption("EMERGENCY");
    await page
      .getByLabel("Opis skrócony", { exact: true })
      .fill(`Pilna naprawa ${suffix}`);
    await page
      .getByLabel("Opis zmiany", { exact: true })
      .fill("Pilna stabilizacja po awarii");
    await choose(page, "Opublikowany proces akceptacji", emergency);
    for (const label of [
      "Uzasadnienie zmiany",
      "Zakres zmiany",
      "Plan wdrożenia",
      "Plan testów",
      "Plan wycofania",
      "Uzasadnienie pilności",
    ])
      await page
        .getByLabel(label, { exact: true })
        .fill(`Pilna ścieżka: ${label}`);
    await page
      .getByLabel("Planowany początek", { exact: true })
      .fill(wall(date));
    await page.getByLabel("Planowany koniec", { exact: true }).fill(wall(end));
    await page
      .getByRole("button", { name: "Utwórz zmianę", exact: true })
      .click();
    await expect(page).toHaveURL(/\/changes\/[0-9]+$/);
    const emergencyID = new URL(page.url()).pathname.split("/").at(-1)!;
    await page
      .getByRole("tab", { name: "Powiązane rekordy", exact: true })
      .click();
    await choose(page, "Element konfiguracji", ci.name);
    await page.getByRole("button", { name: "Powiąż CI", exact: true }).click();
    await expect(
      page.getByRole("status").filter({ hasText: "Zapisano powiązanie CI." }),
    ).toBeVisible();
    await page.getByRole("tab", { name: "Szczegóły", exact: true }).click();
    await page
      .getByLabel("Przejście procesu", { exact: true })
      .selectOption("AUTHORIZE");
    await page
      .getByRole("button", { name: "Wykonaj przejście", exact: true })
      .click();
    await expect(page.locator(".record-facts")).toContainText("Autoryzacja");
    c = await (await request.get(`/api/v1/changes/${emergencyID}`)).json();
    await expect
      .poll(
        async () =>
          (
            await (
              await request.get(`/api/v1/workflow/executions/${c.execution_id}`)
            ).json()
          ).state,
        { timeout: 30000 },
      )
      .toBe("WAITING");
    await ap.goto(`/approvals/${c.execution_id}`);
    await ap
      .getByRole("combobox", { name: "Decyzja", exact: true })
      .selectOption("REJECTED");
    await ap
      .getByLabel("Uzasadnienie", { exact: true })
      .fill("Ryzyko wymaga uzupełnienia pilnego planu");
    await ap
      .getByRole("button", { name: "Zapisz decyzję", exact: true })
      .click();
    await expect
      .poll(
        async () =>
          (await (await request.get(`/api/v1/changes/${emergencyID}`)).json())
            .approval_state,
        { timeout: 30000 },
      )
      .toBe("REJECTED");
    expect(
      (await (await request.get(`/api/v1/changes/${emergencyID}`)).json())
        .state,
    ).toBe("NEW");
  } finally {
    await context.close();
  }
});
