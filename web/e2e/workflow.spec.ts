import {
  test,
  expect,
  type APIRequestContext,
  type Page,
} from "@playwright/test";
import {
  loginAPI,
  permanentRootPassword,
  changeTemporaryPassword,
  personalPassword,
} from "./account-fixtures";
const tenant = process.env.E2E_TENANT ?? "e2e",
  temporary = "workflow-temporary-password";
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
test("workflow approval preserves proposal, pins version and creates exactly one real fulfillment task", async ({
  page,
  browser,
  request,
}, info) => {
  test.setTimeout(240000);
  page.setDefaultTimeout(15000);
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
  const me = await (await request.get("/api/v1/auth/me")).json();
  async function group(name: string) {
    const r = await request.post("/api/v1/admin/rbac/groups", {
      headers,
      data: { name },
    });
    expect(r.status()).toBe(201);
    return (await r.json()).id as string;
  }
  const teamName = `Workflow realization ${scope}`,
    approvalName = `Workflow approvals ${scope}`,
    team = await group(teamName),
    approvalGroup = await group(approvalName);
  for (const id of [team, approvalGroup])
    expect(
      (
        await request.post(
          `/api/v1/admin/rbac/groups/${id}/members/${me.user.id}`,
          { headers },
        )
      ).status(),
    ).toBe(204);
  async function actor(name: string, permissions: string[]) {
    const email = `workflow-${name}-${scope}@example.test`;
    const u = await request.post("/api/v1/admin/users/", {
      headers,
      data: { email, display_name: `${name} ${scope}`, password: temporary },
    });
    expect(u.status()).toBe(201);
    const id = (await u.json()).id as string;
    const role = await request.post("/api/v1/admin/rbac/roles", {
      headers,
      data: { name: `workflow-${name}-${scope}`, permissions },
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
    const context = await browser.newContext({
      viewport: page.viewportSize() ?? undefined,
      baseURL: process.env.E2E_BASE_URL ?? "http://127.0.0.1:5173",
    });
    const p = await context.newPage();
    expect(
      (
        await loginAPI(context.request, { tenant, email, password: temporary })
      ).status(),
    ).toBe(200);
    await p.goto("/account");
    await changeTemporaryPassword(p, temporary, personalPassword(temporary));
    return { id, email, context, page: p };
  }
  const reporter = await actor("requester", [
      "catalog.read",
      "request.create",
      "request.read_own",
      "request.cancel_own",
    ]),
    approver = await actor("approver", ["workflow.approve"]);
  expect(
    (
      await request.post(
        `/api/v1/admin/rbac/groups/${approvalGroup}/members/${approver.id}`,
        { headers },
      )
    ).status(),
  ).toBe(204);
  await page.context().addCookies((await request.storageState()).cookies);
  await page.goto("/workflow");
  await page.getByRole("button", { name: "Nowy proces", exact: true }).click();
  const processName = `Workflow ${scope}`;
  await page.getByLabel("Nazwa procesu", { exact: true }).fill(processName);
  await choose(page, "Tożsamość wykonawcza", me.user.display_name);
  await choose(page, "Grupa realizacji procesu", teamName);
  await choose(page, "Akceptujący", approvalName);
  await page
    .getByRole("button", { name: "Zapisz szkic procesu", exact: true })
    .click();
  await expect(page).toHaveURL(/workflow\?.*id=/);
  const definitionID = new URL(page.url()).searchParams.get("id")!;
  if (process.env.E2E_BIGINT === "1")
    expect(BigInt(definitionID)).toBeGreaterThan(9007199254740991n);
  await page
    .getByRole("button", { name: "Zweryfikuj i opublikuj", exact: true })
    .click();
  await expect(page.getByText(/opublikowana wersja 1/)).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: info.outputPath("workflow-definition.png"),
    fullPage: true,
  });
  const cat = await request.post("/api/v1/catalog/admin/catalogs", {
    headers: { ...headers, "Idempotency-Key": `workflow-catalog-${scope}` },
    data: { name: `Workflow catalog ${scope}`, description: "" },
  });
  expect(cat.status()).toBe(200);
  const category = await request.post("/api/v1/catalog/admin/categories", {
    headers: { ...headers, "Idempotency-Key": `workflow-category-${scope}` },
    data: {
      name: `Workflow category ${scope}`,
      description: "",
      catalog_id: (await cat.json()).id,
    },
  });
  expect(category.status()).toBe(200);
  const categoryName = (await category.json()).name as string;
  await page.goto("/catalog-admin");
  await page.getByRole("button", { name: "Nowa usługa", exact: true }).click();
  await page
    .getByLabel("Kategoria", { exact: true })
    .selectOption({ label: categoryName });
  const serviceName = `Workflow service ${scope}`;
  await page.getByLabel("Nazwa usługi").fill(serviceName);
  await page.getByLabel("Sposób realizacji").selectOption("WORKFLOW");
  await page.getByLabel("Wyszukaj opublikowany proces").fill(processName);
  await page
    .getByLabel("Wersja procesu", { exact: true })
    .selectOption({ label: `${processName} · wersja 1` });
  await page.getByRole("button", { name: "Zapisz szkic", exact: true }).click();
  await expect(page).toHaveURL(/catalog-admin\?.*id=/);
  const itemID = new URL(page.url()).searchParams.get("id")!;
  await page.getByRole("button", { name: "Opublikuj zapisaną wersję" }).click();
  await expect(page.getByText(/publikacja 1/)).toBeVisible();
  await reporter.page.goto(`/catalog/${itemID}`);
  await reporter.page
    .getByRole("button", { name: "Złóż wniosek", exact: true })
    .click();
  await expect(reporter.page).toHaveURL(/requests\/[0-9]+/);
  const requestID = new URL(reporter.page.url()).pathname.split("/").at(-1)!;
  const submitted = await (
    await request.get(`/api/v1/requests/${requestID}`)
  ).json();
  expect(submitted.items[0].total_tasks).toBe(0);
  let executionID = "";
  await expect
    .poll(
      async () => {
        const p = await (
          await request.get(
            `/api/v1/workflow/executions?request_id=${requestID}`,
          )
        ).json();
        executionID = p.items[0]?.id ?? "";
        return p.items[0]?.state;
      },
      { timeout: 30000 },
    )
    .toBe("WAITING");
  if (process.env.E2E_BIGINT === "1")
    expect(BigInt(executionID)).toBeGreaterThan(9007199254740991n);
  // Publish a changed task title while the original request waits.
  await page.goto(`/workflow?id=${definitionID}`);
  await page
    .getByLabel("Tytuł zadania", { exact: true })
    .fill("NEW VERSION MUST NOT APPLY");
  await page
    .getByRole("button", { name: "Zapisz szkic procesu", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Zweryfikuj i opublikuj", exact: true }),
  ).toBeEnabled();
  await page
    .getByRole("button", { name: "Zweryfikuj i opublikuj", exact: true })
    .click();
  await expect(page.getByText(/opublikowana wersja 3/)).toBeVisible();
  await approver.page.goto("/approvals");
  await approver.page
    .getByRole("button", { name: "Odśwież akceptacje" })
    .click();
  await approver.page
    .getByRole("link", { name: submitted.number, exact: true })
    .click();
  await expect(
    approver.page.getByRole("button", { name: "Zapisz decyzję" }),
  ).toBeVisible();
  await approver.page
    .getByLabel("Uzasadnienie", { exact: true })
    .fill("LOCAL DECISION MUST SURVIVE CONFLICT");
  const waiting = await (
    await request.get(`/api/v1/workflow/executions/${executionID}`)
  ).json();
  const version = waiting.steps.find(
    (s: { node_id: string }) => s.node_id === "approval",
  ).version;
  expect(
    (
      await request.post(
        `/api/v1/workflow/executions/${executionID}/decisions/approval`,
        {
          headers,
          data: {
            version,
            decision: "APPROVED",
            reason: "Concurrent administrator acceptance",
          },
        },
      )
    ).status(),
  ).toBe(200);
  await approver.page.getByRole("button", { name: "Zapisz decyzję" }).click();
  await expect(
    approver.page.getByRole("heading", { name: "Konflikt decyzji lub wersji" }),
  ).toBeVisible();
  await expect(
    approver.page.getByLabel("Uzasadnienie", { exact: true }),
  ).toHaveValue("LOCAL DECISION MUST SURVIVE CONFLICT");
  await approver.page.screenshot({
    path: info.outputPath("approval-conflict.png"),
    fullPage: true,
  });
  // A confirmed backend decision cannot create a second task on retry.
  expect(
    (
      await request.post(
        `/api/v1/workflow/executions/${executionID}/decisions/approval`,
        {
          headers,
          data: {
            version,
            decision: "APPROVED",
            reason: "Concurrent administrator acceptance",
          },
        },
      )
    ).status(),
  ).toBe(200);
  let taskID = "";
  await expect
    .poll(
      async () => {
        const p = await (
          await request.get("/api/v1/fulfillment/?view=group&limit=100")
        ).json();
        const tasks = p.items.filter(
          (t: { request_id: string }) => t.request_id === requestID,
        );
        taskID = tasks[0]?.id ?? "";
        return tasks.length;
      },
      { timeout: 30000 },
    )
    .toBe(1);
  const task = await (
    await request.get(`/api/v1/fulfillment/${taskID}`)
  ).json();
  expect(task.title).toBe("Realizacja usługi");
  expect(
    (
      await (
        await request.get(`/api/v1/workflow/executions/${executionID}`)
      ).json()
    ).definition_version,
  ).toBe("1");
  await page.goto(`/fulfillment/${taskID}?tab=actions`);
  await page
    .getByRole("button", { name: "Przypisz do siebie", exact: true })
    .click();
  await page
    .getByLabel("Nowy stan", { exact: true })
    .selectOption("IN_PROGRESS");
  await page.getByRole("button", { name: "Zapisz stan", exact: true }).click();
  await page.getByLabel("Nowy stan", { exact: true }).selectOption("COMPLETED");
  await page
    .getByLabel("Opis wyniku lub uzasadnienie")
    .fill("Provisioned after approval");
  await page.getByRole("button", { name: "Zapisz stan", exact: true }).click();
  await expect
    .poll(
      async () =>
        (
          await (
            await request.get(`/api/v1/workflow/executions/${executionID}`)
          ).json()
        ).state,
      { timeout: 90000 },
    )
    .toBe("COMPLETED");
  expect(
    (await (await request.get(`/api/v1/requests/${requestID}`)).json()).state,
  ).toBe("FULFILLED");
  await reporter.page.goto(`/approvals/${executionID}`);
  await expect(
    reporter.page.getByRole("list", { name: "Stan procesu" }),
  ).toBeVisible();
  expect(
    await reporter.page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await reporter.page.screenshot({
    path: info.outputPath("workflow-completed.png"),
    fullPage: true,
  });
  await expect
    .poll(
      async () => {
        const p = await (
          await reporter.context.request.get("/api/v1/notifications/?limit=100")
        ).json();
        return p.notifications.some(
          (n: { execution_id?: string }) => n.execution_id === executionID,
        );
      },
      { timeout: 30000 },
    )
    .toBe(true);
  await page.goto(`/approvals/${executionID}`);
  await expect(
    page.getByRole("heading", { name: "Historia prób (ostatnie 100)" }),
  ).toBeVisible();
  await page.screenshot({
    path: info.outputPath("workflow-attempts.png"),
    fullPage: true,
  });
  // Diagnose a real stopped worker step, restore its identity and resume in UI.
  const second = await reporter.context.request.post("/api/v1/requests/", {
    headers: {
      ...(await csrf(reporter.context.request)),
      "Idempotency-Key": `workflow-repair-${scope}`,
    },
    data: { items: [{ item_id: itemID, version: "1", values: {} }] },
  });
  expect(second.status()).toBe(201);
  const secondRequest = await second.json();
  let secondExecution = "";
  await expect
    .poll(
      async () => {
        const p = await (
          await request.get(
            `/api/v1/workflow/executions?request_id=${secondRequest.id}`,
          )
        ).json();
        secondExecution = p.items[0]?.id ?? "";
        return p.items[0]?.state;
      },
      { timeout: 30000 },
    )
    .toBe("WAITING");
  const secondWaiting = await (
    await request.get(`/api/v1/workflow/executions/${secondExecution}`)
  ).json();
  expect(
    (
      await request.post(
        `/api/v1/workflow/executions/${secondExecution}/decisions/approval`,
        {
          headers,
          data: {
            version: secondWaiting.steps.find(
              (s: { node_id: string }) => s.node_id === "approval",
            ).version,
            decision: "APPROVED",
            reason: "Prepare durable task wait for repair",
          },
        },
      )
    ).status(),
  ).toBe(200);
  await expect
    .poll(
      async () => {
        const x = await (
          await request.get(`/api/v1/workflow/executions/${secondExecution}`)
        ).json();
        return `${x.state}:${x.current_node}`;
      },
      { timeout: 30000 },
    )
    .toBe("WAITING:wait");
  expect(
    (
      await request.delete(
        `/api/v1/admin/rbac/groups/${team}/members/${me.user.id}`,
        { headers },
      )
    ).status(),
  ).toBe(204);
  await expect
    .poll(
      async () =>
        (
          await (
            await request.get(`/api/v1/workflow/executions/${secondExecution}`)
          ).json()
        ).state,
      { timeout: 70000 },
    )
    .toBe("ERROR");
  await page.goto(`/approvals/${secondExecution}`);
  await expect(
    page.getByText(/Wykonawca utracił aktywność/).first(),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Wznów bieżący krok" }),
  ).toBeDisabled();
  expect(
    (
      await request.post(
        `/api/v1/admin/rbac/groups/${team}/members/${me.user.id}`,
        { headers },
      )
    ).status(),
  ).toBe(204);
  await page
    .getByLabel("Uzasadnienie", { exact: true })
    .fill("Restored verified execution membership");
  await page.getByRole("button", { name: "Wznów bieżący krok" }).click();
  await expect
    .poll(
      async () =>
        (
          await (
            await request.get(`/api/v1/workflow/executions/${secondExecution}`)
          ).json()
        ).state,
      { timeout: 30000 },
    )
    .toBe("WAITING");
  await page.getByRole("button", { name: "Odśwież stan procesu" }).click();
  await page
    .getByLabel("Uzasadnienie", { exact: true })
    .fill("Cancel execution with explicit administrative reason");
  page.once("dialog", (dialog) => dialog.accept());
  await page
    .getByRole("button", { name: "Anuluj proces", exact: true })
    .click();
  await expect
    .poll(
      async () =>
        (
          await (
            await request.get(`/api/v1/workflow/executions/${secondExecution}`)
          ).json()
        ).state,
    )
    .toBe("CANCELLED");
  expect(
    (await (await request.get(`/api/v1/requests/${secondRequest.id}`)).json())
      .state,
  ).toBe("SUBMITTED");
  await page.screenshot({
    path: info.outputPath("workflow-repaired-and-cancelled.png"),
    fullPage: true,
  });
  await reporter.context.close();
  await approver.context.close();
});
