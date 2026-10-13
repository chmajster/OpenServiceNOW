import {
  expect,
  test,
  type APIRequestContext,
  type Page,
} from "@playwright/test";
import { writeFile, access } from "node:fs/promises";
import {
  loginAPI,
  permanentRootPassword,
  changeTemporaryPassword,
  personalPassword,
} from "./account-fixtures";
const tenant = process.env.E2E_TENANT ?? "e2e",
  temporary = "change-temporary-password";
async function keyed(request: APIRequestContext) {
  return {
    "X-CSRF-Token": (await request.storageState()).cookies.find(
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
async function transition(p: Page, state: string, label: string) {
  await p
    .getByLabel("Uzasadnienie", { exact: true })
    .fill("Jawna decyzja operatora o przejściu procesu");
  await p.getByLabel("Przejście procesu", { exact: true }).selectOption(state);
  await p
    .getByRole("button", { name: "Wykonaj przejście", exact: true })
    .click();
  await expect(p.locator(".record-facts")).toContainText(label);
}
test("Integrated ITSM workspace retains incident privacy through Problem, approved Change and verification", async ({
  page,
  browser,
  request,
}, info) => {
  test.setTimeout(360000);
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
    headers = await keyed(request),
    me = await (await request.get("/api/v1/auth/me")).json();
  const teamName = `Wdrożenie ${suffix}`,
    approvalName = `CAB ${suffix}`;
  async function group(name: string) {
    const r = await request.post("/api/v1/admin/rbac/groups", {
      headers,
      data: { name },
    });
    expect(r.status()).toBe(201);
    return (await r.json()).id as string;
  }
  const team = await group(teamName),
    cab = await group(approvalName);
  async function actor(name: string, permissions: string[]) {
    const email = `change-${name}-${suffix}@example.test`,
      r = await request.post("/api/v1/admin/users/", {
        headers,
        data: { email, display_name: `${name} ${suffix}`, password: temporary },
      });
    expect(r.status()).toBe(201);
    const id = (await r.json()).id as string,
      role = await request.post("/api/v1/admin/rbac/roles", {
        headers,
        data: { name: `${name}-${suffix}`, permissions },
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
        baseURL: process.env.E2E_BASE_URL,
        viewport: page.viewportSize() ?? undefined,
        isMobile: info.project.name === "mobile-chromium",
        hasTouch: info.project.name === "mobile-chromium",
      }),
      p = await context.newPage();
    p.setDefaultTimeout(12000);
    expect(
      (
        await loginAPI(context.request, { tenant, email, password: temporary })
      ).status(),
    ).toBe(200);
    await p.goto("/account");
    await changeTemporaryPassword(p, temporary, personalPassword(temporary));
    return { id, name: `${name} ${suffix}`, context, page: p };
  }
  const op = await actor("Operator", [
      "incident.create",
      "incident.read",
      "incident.update",
      "incident.assign",
      "incident.notes.read",
      "incident.notes.write",
      "problem.create",
      "problem.read_own",
      "problem.manage",
      "problem.assign",
      "problem.transition",
      "problem.known_error",
      "problem.notes",
      "problem.tasks",
      "change.read_own",
      "change.create",
      "change.manage",
      "change.assign",
      "change.transition",
      "change.execute",
      "change.notes",
      "change.tasks",
      "workflow.execute",
      "cmdb.read",
      "cmdb.link",
    ]),
    approver = await actor("Akceptujący", [
      "workflow.approve",
      "change.read",
      "change.template_approve",
      "cmdb.read",
    ]);
  try {
    for (const [g, u] of [
      [team, op.id],
      [cab, approver.id],
    ])
      expect(
        (
          await request.post(`/api/v1/admin/rbac/groups/${g}/members/${u}`, {
            headers,
          })
        ).status(),
      ).toBe(204);
    await page.context().addCookies((await request.storageState()).cookies);
    async function screens(target: Page, name: string) {
      await expect(target.locator(".page-heading h1")).toBeVisible();
      const original = target.viewportSize();
      for (const width of info.project.name === "chromium"
        ? [1366, 1920]
        : [412]) {
        await target.setViewportSize({ width, height: 900 });
        expect(
          await target.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth,
          ),
        ).toBe(true);
        await target.evaluate(() => {
          (document.activeElement as HTMLElement)?.blur();
          window.scrollTo(0, 0);
        });
        await target.screenshot({
          path: info.outputPath(`${name}-${width}.png`),
          fullPage: info.project.name !== "mobile-chromium",
        });
      }
      if (original) await target.setViewportSize(original);
    }
    await page.goto("/cmdb/new");
    await page.getByLabel("Klasa CI", { exact: true }).selectOption("DATABASE");
    await page
      .getByLabel("Identyfikator CI", { exact: true })
      .fill(`change-db-${suffix}`);
    await page.getByLabel("Nazwa CI", { exact: true }).fill(`Baza ${suffix}`);
    await page
      .getByLabel("Stan operacyjny", { exact: true })
      .selectOption("ACTIVE");
    await page
      .getByLabel("Etap cyklu życia", { exact: true })
      .selectOption("OPERATE");
    await page.getByLabel("Silnik bazy", { exact: true }).fill("PostgreSQL");
    await choose(page, "Grupa wsparcia", teamName);
    await page.getByRole("button", { name: "Utwórz CI", exact: true }).click();
    await expect(page).toHaveURL(/\/cmdb\/[0-9]+$/);
    const ciID = new URL(page.url()).pathname.split("/").at(-1)!;
    const ci = await (await request.get(`/api/v1/cmdb/cis/${ciID}`)).json();
    await screens(page, "ci");
    await page.goto("/cmdb/new");
    await page.getByLabel("Klasa CI", { exact: true }).selectOption("SERVICE");
    await page
      .getByLabel("Identyfikator CI", { exact: true })
      .fill(`business-service-${suffix}`);
    await page
      .getByLabel("Nazwa CI", { exact: true })
      .fill(`Usługa biznesowa ${suffix}`);
    await page
      .getByLabel("Stan operacyjny", { exact: true })
      .selectOption("ACTIVE");
    await page.getByRole("button", { name: "Utwórz CI", exact: true }).click();
    await expect(page).toHaveURL(/\/cmdb\/[0-9]+$/);
    const serviceID = new URL(page.url()).pathname.split("/").at(-1)!;
    await page.getByRole("tab", { name: "Zależności", exact: true }).click();
    await choose(page, "Docelowy CI", ci.name);
    await page
      .getByLabel("Typ zależności", { exact: true })
      .selectOption("DEPENDS_ON");
    await page
      .getByRole("button", { name: "Dodaj zależność", exact: true })
      .click();
    await expect(page.locator("table")).toContainText(ci.name);
    const graph = await (
      await request.get(`/api/v1/cmdb/cis/${serviceID}/graph`)
    ).json();
    expect(graph.nodes).toHaveLength(2);
    expect(graph.edges[0].source_id).toBe(serviceID);
    expect(graph.edges[0].target_id).toBe(ci.id);
    await page.goto("/workflow");
    await page
      .getByRole("button", { name: "Nowy proces", exact: true })
      .click();
    await page
      .getByRole("combobox", { name: "Domena procesu", exact: true })
      .selectOption("CHANGE");
    const processName = `Ocena ${suffix}`;
    await page.getByLabel("Nazwa procesu", { exact: true }).fill(processName);
    await choose(page, "Tożsamość wykonawcza", op.name);
    await choose(page, "Grupa realizacji procesu", teamName);
    await choose(page, "Akceptujący", approvalName);
    await page
      .getByRole("button", { name: "Zapisz szkic procesu", exact: true })
      .click();
    await expect(page).toHaveURL(/workflow\?.*id=/);
    await page
      .getByRole("button", { name: "Zweryfikuj i opublikuj", exact: true })
      .click();
    await expect(page.getByText(/opublikowana wersja 1/)).toBeVisible();
    const p = op.page,
      r = op.context.request;
    const incidentTitle = `Naprawa ${suffix}`;
    await p.goto("/incidents/new");
    await p.getByLabel("Tytuł", { exact: true }).fill(incidentTitle);
    await p
      .getByLabel("Opis", { exact: true })
      .fill("Powtarzalna awaria połączeń bazy");
    await p.getByLabel("Kategoria", { exact: true }).selectOption("software");
    await p
      .getByLabel("Podkategoria", { exact: true })
      .selectOption("application");
    await p.getByLabel("Wpływ", { exact: true }).selectOption("1");
    await p.getByLabel("Pilność", { exact: true }).selectOption("2");
    await p
      .getByRole("button", { name: "Utwórz incydent", exact: true })
      .click();
    await p.getByRole("link", { name: /^Otwórz INC/ }).click();
    await expect(p).toHaveURL(/\/incidents\/[0-9]+$/);
    const incidentID = new URL(p.url()).pathname.split("/").at(-1)!;
    let incident = await (
      await r.get(`/api/v1/incidents/${incidentID}`)
    ).json();
    await p.goto(`/incidents/${incidentID}?tab=cmdb`);
    await choose(p, "Element konfiguracji", ci.name);
    await p.getByRole("button", { name: "Powiąż CI", exact: true }).click();
    await expect(
      p.getByRole("status").filter({ hasText: "Zapisano powiązanie CI." }),
    ).toBeVisible();
    await p.goto(
      `/incidents?q=${encodeURIComponent(incident.number)}&view=all`,
    );
    await expect(p.locator(".record-table")).toContainText(incident.number);
    await screens(p, "incident-list");
    await p.getByRole("link", { name: incident.number, exact: true }).click();
    await screens(p, "incident");
    await p.goto(`/incidents/${incidentID}?tab=communication`);
    await p
      .getByRole("button", { name: "Notatka wewnętrzna", exact: true })
      .click();
    const privateNote = `Poufne dane incydentu ${suffix}`;
    await p
      .getByLabel("Treść notatki wewnętrznej", { exact: true })
      .fill(privateNote);
    await p
      .getByRole("button", { name: "Zapisz notatkę wewnętrzną", exact: true })
      .click();
    await expect(p.getByText(privateNote, { exact: true })).toBeVisible();
    await p.goto(`/incidents/${incidentID}?tab=problems`);
    await p
      .getByRole("link", { name: "Utwórz problem z incydentu", exact: true })
      .click();
    await expect(
      p.getByRole("heading", { name: "Podgląd informacji z incydentu" }),
    ).toBeVisible();
    await expect(p.locator("main")).not.toContainText(privateNote);
    await p
      .getByRole("button", {
        name: "Użyj pokazanych informacji i powiąż incydent",
        exact: true,
      })
      .click();
    await choose(p, "Grupa odpowiedzialna", teamName);
    await choose(p, "Przypisany analityk", op.name);
    await p
      .getByRole("button", { name: "Utwórz problem", exact: true })
      .click();
    await expect(p).toHaveURL(/\/problems\/[0-9]+$/);
    const problemID = new URL(p.url()).pathname.split("/").at(-1)!;
    let problem = await (await r.get(`/api/v1/problems/${problemID}`)).json();
    expect(
      (await (await r.get(`/api/v1/problems/${problemID}/messages`)).json())
        .messages,
    ).toHaveLength(0);
    async function problemTransition(state: string, label: string) {
      await p
        .getByLabel("Przejście procesu", { exact: true })
        .selectOption(state);
      await p
        .getByRole("button", { name: "Wykonaj przejście", exact: true })
        .click();
      await expect(p.locator(".record-facts")).toContainText(label);
    }
    await problemTransition("ASSESS", "Ocena");
    await p
      .getByLabel("Objawy", { exact: true })
      .fill("Timeout pod obciążeniem");
    await p
      .getByRole("button", { name: "Zapisz problem", exact: true })
      .click();
    await expect(
      p.getByRole("button", { name: "Zapisz problem", exact: true }),
    ).toBeDisabled();
    await problemTransition("ROOT_CAUSE", "Analiza przyczyny");
    for (const [label, value] of [
      ["Wyniki analizy", "Odtworzono awarię pod obciążeniem"],
      ["Przyczyna źródłowa", "Wyczerpana pula połączeń"],
      ["Obejście", "Ograniczenie ruchu"],
      ["Plan trwałego rozwiązania", "Zwiększenie puli i test obciążenia"],
    ]) {
      await p.getByLabel(label, { exact: true }).fill(value);
    }
    await p.getByLabel("Known Error", { exact: false }).check();
    await p
      .getByRole("button", { name: "Zapisz problem", exact: true })
      .click();
    await expect(
      p.getByRole("button", { name: "Zapisz problem", exact: true }),
    ).toBeDisabled();
    await problemTransition("FIX_IN_PROGRESS", "Przygotowanie rozwiązania");
    await screens(p, "problem");
    await p
      .getByRole("tab", { name: "Powiązane rekordy", exact: true })
      .click();
    await choose(p, "Element konfiguracji", ci.name);
    await p.getByRole("button", { name: "Powiąż CI", exact: true }).click();
    await expect(
      p.getByRole("status").filter({ hasText: "Zapisano powiązanie CI." }),
    ).toBeVisible();
    problem = await (await r.get(`/api/v1/problems/${problemID}`)).json();
    const note = await r.post(`/api/v1/problems/${problem.id}/messages`, {
      headers: await keyed(r),
      data: { type: "WORK_NOTE", body: `Poufna analiza ${suffix}` },
    });
    expect(note.status()).toBe(200);
    await p.goto(`/problems/${problem.id}?tab=related`);
    await p
      .getByRole("link", {
        name: "Utwórz zmianę naprawczą z podglądem",
        exact: true,
      })
      .click();
    await expect(p.locator("main")).not.toContainText(
      `Poufna analiza ${suffix}`,
    );
    await p
      .getByRole("button", {
        name: "Użyj pokazanych informacji i powiąż problem",
        exact: true,
      })
      .click();
    await choose(p, "Grupa odpowiedzialna", teamName);
    await choose(p, "Opublikowany proces akceptacji", processName);
    await p
      .getByLabel("Uzasadnienie zmiany", { exact: true })
      .fill("Usunięcie przyczyny awarii");
    await p
      .getByLabel("Zakres zmiany", { exact: true })
      .fill("Konfiguracja puli bazy");
    await p.getByLabel("Plan testów", { exact: true }).fill("Test 100 sesji");
    await p
      .getByLabel("Plan wycofania", { exact: true })
      .fill("Przywrócenie poprzednich parametrów");
    const start = new Date(
        Date.now() +
          (process.env.E2E_RESTART_SIGNAL_DIR &&
          info.project.name === "chromium"
            ? 180000
            : 100000),
      ),
      end = new Date(start.getTime() + 3600000);
    const wall = (d: Date) =>
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
    // Minutes are the displayed scheduling precision; use the next whole minute.
    start.setUTCSeconds(0, 0);
    end.setUTCSeconds(0, 0);
    await p.getByLabel("Planowany początek", { exact: true }).fill(wall(start));
    await p.getByLabel("Planowany koniec", { exact: true }).fill(wall(end));
    let lost = false;
    await p.route("**/api/v1/changes/", async (route) => {
      if (route.request().method() === "POST" && !lost) {
        lost = true;
        await route.fetch();
        await route.abort("failed");
      } else await route.continue();
    });
    await p.getByRole("button", { name: "Utwórz zmianę", exact: true }).click();
    await p
      .getByRole("button", { name: "Potwierdź wynik zapisu", exact: true })
      .click();
    await expect(p).toHaveURL(/\/changes\/[0-9]+$/);
    const id = new URL(p.url()).pathname.split("/").at(-1)!;
    if (process.env.E2E_ASSERT_BIGINT)
      expect(BigInt(id)).toBeGreaterThan(9007199254740991n);
    await p.unroute("**/api/v1/changes/");
    await p
      .getByRole("tab", { name: "Powiązane rekordy", exact: true })
      .click();
    await choose(p, "Element konfiguracji", ci.name);
    let lostCI = false;
    await p.route(
      `**/api/v1/cmdb/records/changes/${id}/cis/${ci.id}`,
      async (route) => {
        if (route.request().method() === "PUT" && !lostCI) {
          lostCI = true;
          await route.fetch();
          await route.abort("failed");
        } else await route.continue();
      },
    );
    await p.getByRole("button", { name: "Powiąż CI", exact: true }).click();
    await expect(
      p.getByRole("button", {
        name: "Potwierdź wynik powiązania CI",
        exact: true,
      }),
    ).toBeVisible();
    await p.getByRole("tab", { name: "Szczegóły", exact: true }).click();
    await p
      .getByRole("tab", { name: "Powiązane rekordy", exact: true })
      .click();
    await p
      .getByRole("button", {
        name: "Potwierdź wynik powiązania CI",
        exact: true,
      })
      .click();
    await expect(
      p.getByRole("status").filter({ hasText: "Zapisano powiązanie CI." }),
    ).toBeVisible();
    await p.unroute(`**/api/v1/cmdb/records/changes/${id}/cis/${ci.id}`);
    await p.getByRole("tab", { name: "Szczegóły", exact: true }).click();
    await transition(p, "ASSESS", "Ocena");
    await transition(p, "AUTHORIZE", "Autoryzacja");
    const record = await (await r.get(`/api/v1/changes/${id}`)).json(),
      execution = record.execution_id as string;
    await expect
      .poll(
        async () =>
          (
            await (
              await request.get(`/api/v1/workflow/executions/${execution}`)
            ).json()
          ).state,
        { timeout: 30000 },
      )
      .toBe("WAITING");
    const waiting = await (
        await request.get(`/api/v1/workflow/executions/${execution}`)
      ).json(),
      deadline = waiting.steps.find(
        (s: { type: string }) => s.type === "APPROVAL",
      ).deadline;
    if (
      process.env.E2E_RESTART_SIGNAL_DIR &&
      info.project.name === "chromium"
    ) {
      const dir = process.env.E2E_RESTART_SIGNAL_DIR;
      await writeFile(
        `${dir}/pending.json`,
        JSON.stringify({ id, execution, deadline }),
      );
      console.log("CHANGE_APPROVAL_RESTART_PENDING", execution);
      await expect
        .poll(
          async () => {
            try {
              await access(`${dir}/complete`);
              return true;
            } catch {
              return false;
            }
          },
          { timeout: 90000, intervals: [500] },
        )
        .toBe(true);
    }
    const resumed = await (
      await request.get(`/api/v1/workflow/executions/${execution}`)
    ).json();
    expect(resumed.state).toBe("WAITING");
    expect(
      resumed.steps.find((s: { type: string }) => s.type === "APPROVAL")
        .deadline,
    ).toBe(deadline);
    const ap = approver.page;
    await ap.goto(`/approvals/${execution}`);
    await expect(
      ap.getByRole("heading", { name: /Oceniany zakres zmiany/ }),
    ).toBeVisible();
    await expect(ap.locator("main")).not.toContainText(
      `Poufna analiza ${suffix}`,
    );
    await ap
      .getByLabel("Uzasadnienie", { exact: true })
      .fill("Sprawdzono plan, wycofanie i zakres CI");
    await ap
      .getByRole("button", { name: "Zapisz decyzję", exact: true })
      .click();
    await expect
      .poll(
        async () =>
          (await (await r.get(`/api/v1/changes/${id}`)).json()).approval_state,
        { timeout: 30000 },
      )
      .toBe("APPROVED");
    await p
      .getByRole("button", { name: "Odśwież zmianę", exact: true })
      .click();
    await transition(p, "SCHEDULED", "Zaplanowana");
    await page.goto("/cab");
    await expect(
      page.getByRole("heading", { name: "CAB — ocena zmian", exact: true }),
    ).toBeVisible();
    await page.goto(
      `/change-calendar?date=${wall(start).slice(0, 10)}&mode=week`,
    );
    await expect(
      page.getByRole("link", { name: new RegExp(record.number) }).first(),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await screens(page, "calendar");
    await page.screenshot({
      path: info.outputPath("change-calendar.png"),
      fullPage: info.project.name !== "mobile-chromium",
    });
    await p.getByRole("tab", { name: "Zadania", exact: true }).click();
    await p
      .getByRole("button", { name: "Nowe zadanie wykonawcze", exact: true })
      .click();
    await p
      .getByLabel("Opis zadania", { exact: true })
      .fill("Wdrożenie parametrów puli");
    await p
      .getByLabel("Instrukcje wykonania", { exact: true })
      .fill("Zastosuj zatwierdzone parametry i sprawdź połączenia");
    await p
      .getByRole("button", { name: "Utwórz zadanie wykonawcze", exact: true })
      .click();
    await expect(
      p.getByRole("row").filter({ hasText: "Wdrożenie parametrów puli" }),
    ).toBeVisible();
    await expect
      .poll(() => Date.now() >= start.getTime(), {
        timeout: 110000,
        intervals: [500],
      })
      .toBe(true);
    await p.getByRole("tab", { name: "Szczegóły", exact: true }).click();
    await transition(p, "IMPLEMENT", "Wykonanie");
    await p.getByRole("tab", { name: "Zadania", exact: true }).click();
    await p
      .getByRole("button", { name: "Otwórz zadanie", exact: true })
      .click();
    await p
      .getByLabel("Stan zadania wykonawczego", { exact: true })
      .selectOption("IN_PROGRESS");
    await p
      .getByRole("button", { name: "Zapisz zadanie", exact: true })
      .click();
    await p
      .getByRole("button", { name: "Otwórz zadanie", exact: true })
      .click();
    await p
      .getByLabel("Wynik zadania", { exact: true })
      .fill("Parametry wdrożone, 100 sesji przeszło test");
    await p
      .getByLabel("Stan zadania wykonawczego", { exact: true })
      .selectOption("COMPLETED");
    await p
      .getByRole("button", { name: "Zapisz zadanie", exact: true })
      .click();
    await expect(
      p.getByRole("row").filter({ hasText: "Wdrożenie parametrów puli" }),
    ).toContainText("Ukończone");
    await p.getByRole("tab", { name: "Szczegóły", exact: true }).click();
    await transition(p, "REVIEW", "Przegląd");
    await p
      .getByRole("combobox", { name: "Wynik przeglądu", exact: true })
      .selectOption("SUCCESS");
    await p
      .getByLabel("Opis przeglądu", { exact: true })
      .fill("Brak ponownych awarii po wdrożeniu");
    await p.getByRole("button", { name: "Zapisz zmianę", exact: true }).click();
    await expect(
      p.getByRole("button", { name: "Zapisz zmianę", exact: true }),
    ).toBeDisabled();
    await transition(p, "CLOSED", "Zamknięta");
    const closed = await (await r.get(`/api/v1/changes/${id}`)).json();
    expect(closed.actual_start).toBeTruthy();
    expect(closed.actual_end).toBeTruthy();
    expect(closed.approved_revision).toBe(closed.assessment_revision);
    expect(
      (await (await r.get(`/api/v1/changes/${id}/tasks`)).json()).items,
    ).toHaveLength(1);
    await p.getByRole("tab", { name: "Historia", exact: true }).click();
    await expect
      .poll(
        async () => {
          const history = await (
            await r.get(`/api/v1/changes/${id}/history`)
          ).json();
          return history.entries.some(
            (e: { data: { after?: { state?: string } } }) =>
              e.data.after?.state === "CLOSED",
          );
        },
        { timeout: 20000 },
      )
      .toBe(true);
    await p
      .getByRole("button", { name: "Odśwież historię", exact: true })
      .click();
    await expect(p.locator(".timeline")).toContainText("CLOSED");
    await p.screenshot({
      path: info.outputPath("change-history.png"),
      fullPage: info.project.name !== "mobile-chromium",
    });
    await p.goto(`/problems/${problem.id}?tab=related`);
    await expect(
      p.getByRole("link", { name: new RegExp(record.number) }).first(),
    ).toBeVisible();
    await p.goto(`/cmdb/${ci.id}?tab=records`);
    await expect(
      p.getByRole("link", { name: new RegExp(record.number) }).first(),
    ).toBeVisible();
    await p.goto(`/problems/${problem.id}`);
    await p
      .getByLabel("Wynik weryfikacji", { exact: true })
      .fill("Po zmianie wykonano test obciążenia; brak wyczerpania puli");
    await p
      .getByRole("button", { name: "Zapisz problem", exact: true })
      .click();
    await expect(
      p.getByRole("button", { name: "Zapisz problem", exact: true }),
    ).toBeDisabled();
    await problemTransition("RESOLVED", "Rozwiązany");
    await problemTransition("CLOSED", "Zamknięty");
    await p.reload();
    await expect(p.locator(".record-facts")).toContainText("Zamknięty");
    incident = await (await r.get(`/api/v1/incidents/${incidentID}`)).json();
    expect(incident.state).toBe("NEW");
    await p.goto(`/incidents/${incidentID}?tab=problems`);
    await expect(
      p.getByRole("link", { name: new RegExp(problem.number) }).first(),
    ).toBeVisible();
    await p.goto(`/problems/${problem.id}?tab=history`);
    await expect
      .poll(
        async () =>
          (
            await (await r.get(`/api/v1/problems/${problem.id}/history`)).json()
          ).entries.some(
            (e: { data: { after?: { state?: string } } }) =>
              e.data.after?.state === "CLOSED",
          ),
        { timeout: 20000 },
      )
      .toBe(true);
    await p
      .getByRole("button", { name: "Odśwież historię", exact: true })
      .click();
    await expect(p.locator(".timeline")).toContainText("CLOSED");
    await p.goto(`/changes/${id}`);
    await screens(p, "change");
    await p.goto("/");
    await expect(
      p.getByRole("heading", { name: "Mój obszar pracy", exact: true }),
    ).toBeVisible();
    await screens(p, "workspace");
    for (let n = 0; n < 26; n++) {
      const response = await r.post("/api/v1/changes/", {
        headers: await keyed(r),
        data: {
          title: `Paginacja ${suffix} ${n}`,
          description: "Jawna fixture listy zmian",
          change_type: "NORMAL",
          impact: "MEDIUM",
          risk: "LOW",
          time_zone: "Europe/Warsaw",
        },
      });
      expect(response.status()).toBe(200);
    }
    const query = `q=${encodeURIComponent(`Paginacja ${suffix}`)}&limit=2`,
      first = await (await r.get(`/api/v1/changes/?${query}`)).json();
    expect(first.total).toBe("26");
    expect(first.items).toHaveLength(2);
    const next = await (
      await r.get(`/api/v1/changes/?${query}&before_id=${first.next_before_id}`)
    ).json();
    expect(next.total).toBe("26");
    expect(next.items[0].id).not.toBe(first.items[0].id);
    await p.goto(`/changes?${query}`);
    await expect(p.locator("main")).toContainText("26");
    expect(
      await p.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.goto("/change-calendar/rules/new");
    await page
      .getByLabel("Nazwa reguły", { exact: true })
      .fill(`Okno ${suffix}`);
    await page.getByLabel("Początek reguły", { exact: true }).fill(wall(start));
    await page.getByLabel("Koniec reguły", { exact: true }).fill(wall(end));
    await choose(page, "CI reguły (puste: cała organizacja)", ci.name);
    await page
      .getByRole("button", { name: "Zapisz regułę", exact: true })
      .click();
    await expect(page).toHaveURL(/change-calendar\/rules\/[0-9]+$/);
    await page.getByLabel("Aktywna reguła", { exact: false }).uncheck();
    await page
      .getByRole("button", { name: "Zapisz regułę", exact: true })
      .click();
    await expect(
      page.getByRole("status").filter({ hasText: "Zapisano." }),
    ).toBeVisible();
  } finally {
    await op.context.close();
    await approver.context.close();
  }
});
