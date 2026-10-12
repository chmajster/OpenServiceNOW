import {
  expect,
  test,
  type Page,
  type APIRequestContext,
} from "@playwright/test";
import {
  changeTemporaryPassword,
  loginAPI,
  permanentRootPassword,
  personalPassword,
} from "./account-fixtures";

const tenant = process.env.E2E_TENANT ?? "e2e";
const temporary = "incident-temporary-password";
async function csrf(request: APIRequestContext) {
  return {
    "X-CSRF-Token": (await request.storageState()).cookies.find(
      (c) => c.name === "opensnow_csrf",
    )!.value,
  };
}
async function signIn(page: Page, email: string) {
  await page.goto("/login");
  await page.getByLabel("Organizacja", { exact: true }).fill(tenant);
  await page.getByLabel("Login lub e-mail").fill(email);
  await page.getByLabel("Hasło", { exact: true }).fill(temporary);
  const submit = async () => {
    const result = page.waitForResponse(
      (r) =>
        r.url().endsWith("/api/v1/auth/login") &&
        r.request().method() === "POST",
    );
    await page.getByRole("button", { name: "Zaloguj", exact: true }).click();
    return result;
  };
  let result = await submit();
  if (result.status() === 429) {
    const seconds = Number(result.headers()["retry-after"]);
    expect(seconds).toBeGreaterThan(0);
    expect(seconds).toBeLessThanOrEqual(60);
    await new Promise((resolve) => setTimeout(resolve, seconds * 1000));
    result = await submit();
  }
  expect(result.status()).toBe(200);
  await changeTemporaryPassword(page, temporary, personalPassword(temporary));
}
async function select(page: Page, label: string, search: string, name: string) {
  const input = page.getByRole("combobox", { name: label, exact: true });
  await input.fill(search);
  await expect(page.getByRole("option", { name, exact: true })).toBeVisible();
  await input.press("ArrowDown");
  await input.press("Enter");
  await expect(input).toHaveValue(name);
}
async function noOverflow(page: Page) {
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
}

test("incident UI creates once, assigns, handles, resolves and closes with durable history", async ({
  page,
  browser,
  request,
}, info) => {
  test.setTimeout(240_000);
  page.setDefaultTimeout(10_000);
  const scope = `${info.project.name}-${info.retry}-${Date.now()}`;
  const title = `Awaria VPN ${scope}`;
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
  async function user(email: string, display_name: string) {
    const response = await request.post("/api/v1/admin/users/", {
      headers,
      data: { email, display_name, password: temporary },
    });
    expect(response.status()).toBe(201);
    return (await response.json()).id as string;
  }
  // Standalone execution also puts the real agent beyond the directory's first page.
  const directory = await (
    await request.get(
      "/api/v1/incidents/lookups/users?purpose=assignment&limit=50",
    )
  ).json();
  for (let n = directory.users.length; n < 50; n++)
    await user(`incident-directory-${n}-${scope}@example.test`, `Katalog ${n}`);
  const agentEmail = `incident-agent-${scope}@example.test`,
    reporterEmail = `incident-reporter-${scope}@example.test`;
  const agentName = `Agent ${scope}`,
    reporterName = `Zgłaszający ${scope}`;
  const agentID = await user(agentEmail, agentName),
    reporterID = await user(reporterEmail, reporterName);
  for (const [name, permissions, userID] of [
    [
      `agent-${scope}`,
      [
        "incident.create",
        "incident.read",
        "incident.update",
        "incident.assign",
        "incident.resolve",
      ],
      agentID,
    ],
    [`reporter-${scope}`, ["incident.create", "incident.read_own"], reporterID],
  ] as const) {
    const role = await request.post("/api/v1/admin/rbac/roles", {
      headers,
      data: { name, permissions },
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
  }
  const groupName = `Wsparcie ${scope}`;
  const groupResponse = await request.post("/api/v1/admin/rbac/groups", {
    headers,
    data: { name: groupName },
  });
  expect(groupResponse.status()).toBe(201);
  const groupID = (await groupResponse.json()).id as string;
  expect(
    (
      await request.post(
        `/api/v1/admin/rbac/groups/${groupID}/members/${agentID}`,
        { headers },
      )
    ).status(),
  ).toBe(204);

  await signIn(page, reporterEmail);
  await page.goto("/incidents/new");
  await expect(
    page.getByRole("heading", { name: "Zgłoś incydent", exact: true }),
  ).toBeFocused();
  await page.getByLabel("Tytuł", { exact: true }).fill(title);
  await page
    .getByLabel("Opis", { exact: true })
    .fill("Połączenie VPN nie działa. 🛠️\nPróba na dwóch urządzeniach.");
  await page.getByLabel("Kategoria", { exact: true }).selectOption("network");
  await page.getByLabel("Podkategoria", { exact: true }).selectOption("vpn");
  await page.getByLabel("Wpływ", { exact: true }).selectOption("2");
  await page.getByLabel("Pilność", { exact: true }).selectOption("1");
  await expect(page.getByText("Wyliczony priorytet:")).toContainText("P2");
  // Lose only the response after a real committed API write, then reconcile the same key.
  let committedID = "",
    number = "",
    creationKey = "";
  await page.route("**/api/v1/incidents/", async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    creationKey = route.request().headers()["idempotency-key"]!;
    const result = await route.fetch();
    expect(result.status()).toBe(201);
    const record = await result.json();
    committedID = record.id;
    number = record.number;
    await route.abort("failed");
  });
  await page
    .getByRole("button", { name: "Utwórz incydent", exact: true })
    .click();
  await expect(
    page.getByText("Nie potwierdzono wyniku poprzedniej próby.", {
      exact: false,
    }),
  ).toBeVisible();
  await expect(page.getByLabel("Tytuł", { exact: true })).toHaveValue(title);
  await page.unroute("**/api/v1/incidents/");
  const repeated = page.waitForResponse(
    (r) =>
      r.url().endsWith("/api/v1/incidents/") && r.request().method() === "POST",
  );
  await page
    .getByRole("button", { name: "Sprawdź wynik poprzedniej próby" })
    .click();
  const repeatedResponse = await repeated;
  expect(repeatedResponse.status()).toBe(200);
  expect(repeatedResponse.request().headers()["idempotency-key"]).toBe(
    creationKey,
  );
  expect((await repeatedResponse.json()).id).toBe(committedID);
  await expect(
    page.getByRole("heading", { name: "Incydent zgłoszony" }),
  ).toBeFocused();
  expect(
    (
      await (
        await request.get(`/api/v1/incidents/?q=${encodeURIComponent(title)}`)
      ).json()
    ).incidents,
  ).toHaveLength(1);
  await page.getByRole("link", { name: `Otwórz ${number}` }).click();
  await expect(
    page.getByRole("heading", { name: title, exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Przypisanie", exact: true }),
  ).toHaveCount(0);
  expect(
    (
      await page.request.post(`/api/v1/incidents/${committedID}/transition`, {
        headers: await csrf(page.request),
        data: { version: "1", state: "CLOSED" },
      })
    ).status(),
  ).toBe(403);
  await page.goto("/incidents?view=mine");
  await expect(
    page.getByRole("link", { name: number, exact: true }),
  ).toBeVisible();
  await noOverflow(page);

  const agentContext = await browser.newContext({
    baseURL: process.env.E2E_BASE_URL ?? "http://127.0.0.1:5173",
    ...(info.project.name === "mobile-chromium"
      ? {
          viewport: { width: 412, height: 915 },
          isMobile: true,
          hasTouch: true,
        }
      : {}),
  });
  const agent = await agentContext.newPage();
  agent.setDefaultTimeout(10_000);
  try {
    await signIn(agent, agentEmail);
    const firstChoices = await (
      await agent.request.get(
        "/api/v1/incidents/lookups/users?purpose=assignment&limit=50",
      )
    ).json();
    expect(
      firstChoices.users.some((u: { id: string }) => u.id === agentID),
    ).toBe(false);
    expect(firstChoices.next_after_id).toBeTruthy();
    const privateRecord = await agent.request.post("/api/v1/incidents/", {
      headers: {
        ...(await csrf(agent.request)),
        "Idempotency-Key": `private-${scope}`,
      },
      data: {
        title: `Prywatne ${scope}`,
        description: "Zgłoszenie agenta",
        category: "other",
        subcategory: "other",
        impact: 3,
        urgency: 3,
      },
    });
    expect(privateRecord.status()).toBe(201);
    const privateID = (await privateRecord.json()).id;
    expect(
      (await page.request.get(`/api/v1/incidents/${privateID}`)).status(),
    ).toBe(404);
    expect(
      (
        await page.request.get(`/api/v1/incidents/${privateID}/history`)
      ).status(),
    ).toBe(404);
    await agent.goto(
      `/incidents?view=unassigned&q=${encodeURIComponent(title)}`,
    );
    await agent.getByRole("link", { name: number, exact: true }).click();
    const current = async () =>
      (await agent.request.get(`/api/v1/incidents/${committedID}`)).json();
    expect(
      (
        await agent.request.post(
          `/api/v1/incidents/${committedID}/transition`,
          {
            headers: await csrf(agent.request),
            data: { version: (await current()).version, state: "CLOSED" },
          },
        )
      ).status(),
    ).toBe(422);
    await agent.getByRole("link", { name: "Przypisanie", exact: true }).click();
    await select(
      agent,
      "Osoba przypisana",
      agentEmail,
      `${agentName} — ${agentEmail}`,
    );
    await select(agent, "Grupa obsługi", groupName, groupName);
    await expect(
      agent.getByRole("combobox", { name: "Osoba przypisana", exact: true }),
    ).toHaveValue("");
    await select(
      agent,
      "Osoba przypisana",
      agentEmail,
      `${agentName} — ${agentEmail}`,
    );
    await agent
      .getByRole("button", { name: "Zapisz przypisanie", exact: true })
      .click();
    await expect(
      agent.getByRole("status").filter({ hasText: "Zmiana zapisana" }),
    ).toBeVisible();
    expect((await current()).assignment).toMatchObject({
      assigned_to: agentID,
      assignment_group_id: groupID,
    });
    await agent.goto(`/incidents?view=my_group&q=${encodeURIComponent(title)}`);
    await expect(
      agent.getByRole("link", { name: number, exact: true }),
    ).toBeVisible();
    await agent.getByRole("link", { name: number, exact: true }).click();
    async function transition(state: string, reason?: string) {
      await agent.getByRole("link", { name: "Stan", exact: true }).click();
      await agent.getByLabel("Nowy stan", { exact: true }).selectOption(state);
      if (reason)
        await agent.getByLabel("Powód zmiany", { exact: true }).fill(reason);
      if (state === "RESOLVED") {
        await agent
          .getByLabel("Kod rozwiązania", { exact: true })
          .selectOption("FIXED");
        await agent
          .getByLabel("Opis rozwiązania", { exact: true })
          .fill("Odnowiono certyfikat VPN i sprawdzono połączenie.");
      }
      const result = agent.waitForResponse(
        (r) =>
          r.url().endsWith(`/${committedID}/transition`) &&
          r.request().method() === "POST",
      );
      await agent
        .getByRole("button", { name: "Zmień stan", exact: true })
        .focus();
      await agent.keyboard.press("Enter");
      expect((await result).status()).toBe(200);
      expect((await current()).state).toBe(state);
    }
    await transition("IN_PROGRESS");
    await transition("ON_HOLD", "Oczekiwanie na certyfikat");
    await transition("RESOLVED");
    await transition("CLOSED");
    await agent.reload();
    await expect(
      agent.getByRole("link", { name: "Edycja", exact: true }),
    ).toHaveCount(0);
    await expect(agent.getByText("Zamknięty", { exact: true })).toBeVisible();
    await transition("IN_PROGRESS", "Problem powrócił po ponownym połączeniu");
    let reopened = await current();
    expect(reopened.resolved_at).toBeNull();
    expect(reopened.resolution_description).toBe("");
    expect(
      (
        await agent.request.put(`/api/v1/incidents/${committedID}/priority`, {
          headers: await csrf(agent.request),
          data: { version: reopened.version, priority: 1, reason: "Test" },
        })
      ).status(),
    ).toBe(403);
    const priorityRole = await request.post("/api/v1/admin/rbac/roles", {
      headers,
      data: {
        name: `priority-${scope}`,
        permissions: ["incident.priority.override"],
      },
    });
    expect(priorityRole.status()).toBe(201);
    expect(
      (
        await request.post(
          `/api/v1/admin/rbac/users/${agentID}/roles/${(await priorityRole.json()).id}`,
          { headers },
        )
      ).status(),
    ).toBe(204);
    await agent.reload();
    await agent.getByRole("link", { name: "Priorytet", exact: true }).click();
    await agent
      .getByLabel("Priorytet docelowy", { exact: true })
      .selectOption("1");
    await agent
      .getByRole("button", { name: "Zapisz priorytet", exact: true })
      .click();
    await expect(
      agent.getByRole("alert").filter({ hasText: "Uzasadnienie wymaga" }),
    ).toBeVisible();
    await agent
      .getByLabel("Uzasadnienie priorytetu", { exact: true })
      .fill("Potwierdzony wpływ na usługę krytyczną");
    const override = agent.waitForResponse(
      (r) =>
        r.url().endsWith(`/${committedID}/priority`) &&
        r.request().method() === "PUT",
    );
    await agent
      .getByRole("button", { name: "Zapisz priorytet", exact: true })
      .click();
    expect((await override).status()).toBe(200);
    expect((await current()).priority_override).toBe(1);
    await agent.getByRole("link", { name: "Edycja", exact: true }).click();
    await agent.getByLabel("Wpływ", { exact: true }).selectOption("3");
    const reset = agent.waitForResponse(
      (r) =>
        r.url().endsWith(`/${committedID}`) && r.request().method() === "PUT",
    );
    await agent
      .getByRole("button", { name: "Zapisz dane incydentu", exact: true })
      .click();
    expect((await reset).status()).toBe(200);
    reopened = await current();
    expect(reopened.priority).toBe(3);
    expect(reopened.priority_override).toBeNull();
    // A fresh read of a concurrent edit must not silently rebase the user's proposal.
    await agent.getByRole("link", { name: "Edycja", exact: true }).click();
    await agent
      .getByLabel("Tytuł", { exact: true })
      .fill(`${title} — moja propozycja`);
    const remotelyUpdated = await request.put(
      `/api/v1/incidents/${committedID}`,
      {
        headers,
        data: {
          title: `${title} — aktualny zapis`,
          description: reopened.description,
          affected_user_id: reopened.affected_user_id,
          category: reopened.category,
          subcategory: reopened.subcategory,
          impact: reopened.impact,
          urgency: reopened.urgency,
          version: reopened.version,
        },
      },
    );
    expect(remotelyUpdated.status()).toBe(200);
    await agent
      .getByRole("button", { name: "Zapisz dane incydentu", exact: true })
      .click();
    const conflict = agent.getByRole("region", { name: "Konflikt wersji" });
    await expect(conflict).toContainText(`${title} — aktualny zapis`);
    await expect(conflict).toContainText(`${title} — moja propozycja`);
    await expect(agent.getByLabel("Tytuł", { exact: true })).toHaveValue(
      `${title} — moja propozycja`,
    );
    await expect(
      agent.getByRole("button", { name: "Zapisz dane incydentu", exact: true }),
    ).toBeDisabled();
    await noOverflow(agent);
    await conflict
      .getByRole("button", { name: "Zachowaj propozycję do ponownego zapisu" })
      .click();
    await agent
      .getByRole("button", { name: "Zapisz dane incydentu", exact: true })
      .click();
    await expect(
      agent.getByRole("heading", {
        name: `${title} — moja propozycja`,
        exact: true,
      }),
    ).toBeVisible();
    await transition("RESOLVED");
    // A concurrent close removes write eligibility, but must preserve the draft.
    await agent.getByRole("link", { name: "Edycja", exact: true }).click();
    await agent
      .getByLabel("Opis", { exact: true })
      .fill("Niezapisana diagnoza przed równoległym zamknięciem");
    const readyToClose = await current();
    expect(
      (
        await request.post(`/api/v1/incidents/${committedID}/transition`, {
          headers,
          data: { version: readyToClose.version, state: "CLOSED" },
        })
      ).status(),
    ).toBe(200);
    await agent
      .getByRole("button", { name: "Zapisz dane incydentu", exact: true })
      .click();
    await expect(conflict).toContainText(
      "Niezapisana diagnoza przed równoległym zamknięciem",
    );
    await expect(agent.getByLabel("Opis", { exact: true })).toHaveValue(
      "Niezapisana diagnoza przed równoległym zamknięciem",
    );
    await expect(
      agent
        .getByRole("status")
        .filter({ hasText: "Aktualny stan lub uprawnienia" }),
    ).toBeVisible();
    await expect(
      agent.getByRole("button", { name: "Zapisz dane incydentu", exact: true }),
    ).toBeDisabled();
    await conflict
      .getByRole("button", { name: "Przyjmij aktualny stan" })
      .click();
    await expect(
      agent.getByRole("link", { name: "Edycja", exact: true }),
    ).toHaveCount(0);
    await expect
      .poll(
        async () =>
          (
            await (
              await agent.request.get(
                `/api/v1/incidents/${committedID}/history`,
              )
            ).json()
          ).pending,
        { timeout: 20_000 },
      )
      .toBe(false);
    await agent.getByRole("link", { name: "Historia", exact: true }).click();
    await expect(
      agent.getByRole("heading", {
        name: "Ponownie otwarto incydent",
        exact: true,
      }),
    ).toBeVisible();
    await expect(
      agent.getByRole("heading", { name: "Utworzono incydent", exact: true }),
    ).toBeVisible();
    const history = await (
      await agent.request.get(`/api/v1/incidents/${committedID}/history`)
    ).json();
    expect(
      history.entries.some(
        (entry: { type: string }) => entry.type === "incident.reopened",
      ),
    ).toBe(true);
    for (const entry of history.entries) {
      expect(entry.data.incident_version).toBe(entry.data.after.version);
      expect(entry.event_time).toBe(entry.data.occurred_at);
    }
    await agent.reload();
    await expect(agent).toHaveURL(/tab=history/);
    await noOverflow(agent);
    const path = info.outputPath("incident-history.png");
    await agent.screenshot({ path, fullPage: true });
    await info.attach("incident-history", { path, contentType: "image/png" });
    await page.goto(`/incidents/${committedID}`);
    await expect(page.getByText("Zamknięty", { exact: true })).toBeVisible();
    await expect(
      page.getByRole("heading", {
        name: `${title} — moja propozycja`,
        exact: true,
      }),
    ).toBeVisible();
  } finally {
    await agentContext.close();
  }
});
