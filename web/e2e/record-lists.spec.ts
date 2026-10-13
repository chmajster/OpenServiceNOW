import { expect, test, type APIRequestContext } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { loginAPI, permanentRootPassword } from "./account-fixtures";
const csrf = async (request: APIRequestContext) => ({
  "X-CSRF-Token": (await request.storageState()).cookies.find(
    (c) => c.name === "opensnow_csrf",
  )!.value,
});
test("shared lists filter the full dataset, persist views and export safe authorized data", async ({
  page,
  browser,
}, info) => {
  test.setTimeout(150_000);
  page.setDefaultTimeout(10_000);
  expect(
    (
      await loginAPI(page.request, {
        tenant: process.env.E2E_TENANT ?? "e2e",
        email: process.env.E2E_ADMIN_EMAIL ?? "admin",
        password: permanentRootPassword,
      })
    ).status(),
  ).toBe(200);
  const headers = await csrf(page.request),
    suffix = `${info.project.name}-${Date.now()}`,
    title = `List ${suffix}`;
  const me = await (await page.request.get("/api/v1/auth/me")).json();
  let firstID = "";
  for (let i = 0; i < 31; i++) {
    const created = await page.request.post("/api/v1/incidents/", {
      headers: { ...headers, "Idempotency-Key": crypto.randomUUID() },
      data: {
        title:
          i === 0 ? `=1+1 ${title}` : `${title} ${String(i).padStart(2, "0")}`,
        description: "Real list fixture",
        category: "other",
        subcategory: "other",
        impact: i < 16 ? 1 : 3,
        urgency: i < 16 ? 1 : 3,
      },
    });
    expect(created.status()).toBe(201);
    if (i === 0) firstID = (await created.json()).id;
  }
  await page.goto(`/incidents?q=${encodeURIComponent(title)}&limit=10`);
  const toolbar = page.getByRole("region", { name: "Konfiguracja listy" });
  await expect(
    toolbar.getByText("Dostępnych rekordów: 31", { exact: true }),
  ).toBeVisible();
  await toolbar.getByText("Edytor filtrów AND / OR", { exact: true }).click();
  await toolbar.getByLabel("Pole", { exact: true }).selectOption("priority");
  await toolbar.getByLabel("Wartość", { exact: true }).fill("1");
  await toolbar
    .getByRole("button", { name: "Zastosuj warunek", exact: true })
    .click();
  await expect(
    toolbar.getByText("Dostępnych rekordów: 16", { exact: true }),
  ).toBeVisible();
  await toolbar.getByLabel("Grupowanie", { exact: true }).selectOption("state");
  await expect(
    toolbar.getByRole("button", { name: "Nowy: 16", exact: true }),
  ).toBeVisible();
  await toolbar.getByLabel("Sortowanie", { exact: true }).selectOption("title");
  await expect(page.locator(".record-table tbody tr")).toHaveCount(10);
  await page
    .getByRole("button", { name: "Następna strona", exact: true })
    .click();
  await expect(page.locator(".record-table tbody tr")).toHaveCount(6);
  await expect(
    toolbar.getByText("Dostępnych rekordów: 16", { exact: true }),
  ).toBeVisible();
  await toolbar.getByText("Kolumny i zapisane widoki", { exact: true }).click();
  await toolbar
    .getByRole("button", { name: "Przesuń title w lewo", exact: true })
    .click();
  await expect(page.locator(".record-table thead th").first()).toHaveText(
    "Opis skrócony",
  );
  const group = await page.request.post("/api/v1/admin/rbac/groups", {
    headers,
    data: { name: `Views ${suffix}` },
  });
  expect(group.status()).toBe(201);
  const groupID = (await group.json()).id;
  expect(
    (
      await page.request.post(
        `/api/v1/admin/rbac/groups/${groupID}/members/${me.user.id}`,
        { headers },
      )
    ).status(),
  ).toBe(204);
  const viewName = `Priority one ${suffix}`;
  await toolbar.getByLabel("Nazwa widoku", { exact: true }).fill(viewName);
  const picker = toolbar.getByRole("combobox", {
    name: "Udostępnij mojej grupie",
    exact: true,
  });
  await picker.fill(`Views ${suffix}`);
  await toolbar
    .getByRole("option", { name: `Views ${suffix}`, exact: true })
    .click();
  await toolbar
    .getByRole("button", { name: "Zapisz jako nowy widok", exact: true })
    .click();
  await expect(
    toolbar.getByRole("status").filter({ hasText: "Widok został zapisany" }),
  ).toContainText("Widok został zapisany");
  const saved = (
    await (
      await page.request.get("/api/v1/record-views/?domain=incidents")
    ).json()
  ).items.find((v: { name: string }) => v.name === viewName);
  expect(saved.group_id).toBe(groupID);
  await page.reload();
  await toolbar.getByText("Kolumny i zapisane widoki", { exact: true }).click();
  await toolbar
    .getByLabel("Zapisane widoki", { exact: true })
    .selectOption(saved.id);
  await expect(page.locator(".record-table tbody tr")).toHaveCount(16);
  // Saved presentation does not preserve a pagination cursor; its full filter does persist.
  await expect(
    toolbar.getByText("Dostępnych rekordów: 16", { exact: true }),
  ).toBeVisible();
  await expect(page.locator(".record-table thead th").first()).toHaveText(
    "Opis skrócony",
  );
  const row = page.locator(".record-table tbody tr").first();
  await row.locator("summary").click();
  await row
    .getByRole("button", { name: "Szybki podgląd", exact: true })
    .click();
  await expect(page.getByRole("dialog")).toContainText("Wersja");
  await page
    .getByRole("button", { name: "Zamknij podgląd", exact: true })
    .click();
  const downloaded = page.waitForEvent("download");
  await toolbar
    .getByRole("button", { name: "Eksport CSV", exact: true })
    .click();
  const file = await downloaded;
  const csv = await readFile((await file.path())!, "utf8");
  expect(csv.trim().split("\n")).toHaveLength(17);
  expect(csv).toContain(`'=1+1 ${title}`);
  const forbidden = await page.request.get(
    `/api/v1/incidents/?filter=${encodeURIComponent(JSON.stringify({ field: "i.title; DROP TABLE users", operator: "eq", value: "x" }))}`,
  );
  expect(forbidden.status()).toBe(400);
  // Another group member receives configuration while their own-record policy still applies.
  const email = `view-reader-${suffix}@example.test`,
    temporary = "view-reader-temporary-password";
  const user = await page.request.post("/api/v1/admin/users/", {
    headers,
    data: { email, display_name: "View reader", password: temporary },
  });
  expect(user.status()).toBe(201);
  const userID = (await user.json()).id;
  const role = await page.request.post("/api/v1/admin/rbac/roles", {
    headers,
    data: { name: `view-reader-${suffix}`, permissions: ["incident.read_own"] },
  });
  expect(role.status()).toBe(201);
  const roleID = (await role.json()).id;
  expect(
    (
      await page.request.post(
        `/api/v1/admin/rbac/users/${userID}/roles/${roleID}`,
        { headers },
      )
    ).status(),
  ).toBe(204);
  expect(
    (
      await page.request.post(
        `/api/v1/admin/rbac/groups/${groupID}/members/${userID}`,
        { headers },
      )
    ).status(),
  ).toBe(204);
  const other = await browser.newContext({
    baseURL: process.env.E2E_BASE_URL ?? "http://127.0.0.1:5173",
  });
  try {
    const signed = await loginAPI(other.request, {
      tenant: process.env.E2E_TENANT ?? "e2e",
      email,
      password: temporary,
    });
    expect(signed.status()).toBe(200);
    const session = await signed.json();
    expect(
      (
        await other.request.post("/api/v1/auth/password", {
          headers: await csrf(other.request),
          data: {
            current_password: temporary,
            new_password: `${temporary}-personal`,
            credential_version: session.user.credential_version,
          },
        })
      ).status(),
    ).toBe(200);
    expect(
      (await other.request.get(`/api/v1/record-views/${saved.id}`)).status(),
    ).toBe(200);
    expect(
      (await other.request.get(`/api/v1/incidents/${firstID}`)).status(),
    ).toBe(404);
    const own = await (
      await other.request.get(
        `/api/v1/incidents/?q=${encodeURIComponent(title)}&group_by=state`,
      )
    ).json();
    expect(own.total).toBe("0");
    expect(own.groups).toEqual([]);
    expect(
      (
        await page.request.delete(
          `/api/v1/admin/rbac/groups/${groupID}/members/${userID}`,
          { headers },
        )
      ).status(),
    ).toBe(204);
    expect(
      (await other.request.get(`/api/v1/record-views/${saved.id}`)).status(),
    ).toBe(404);
  } finally {
    await other.close();
  }
  const current = await (
    await page.request.get(`/api/v1/record-views/${saved.id}`)
  ).json();
  expect(
    (
      await page.request.put(`/api/v1/record-views/${saved.id}`, {
        headers,
        data: {
          domain: "incidents",
          name: `Remote ${suffix}`,
          group_id: groupID,
          configuration: current.configuration,
          version: current.version,
        },
      })
    ).status(),
  ).toBe(200);
  await toolbar
    .getByLabel("Nazwa widoku", { exact: true })
    .fill(`Local ${suffix}`);
  await toolbar
    .getByRole("button", { name: "Aktualizuj widok", exact: true })
    .click();
  await expect(
    toolbar.getByRole("region", { name: "Konflikt widoku" }),
  ).toContainText("Moja propozycja");
  await expect(toolbar.getByLabel("Nazwa widoku", { exact: true })).toHaveValue(
    `Local ${suffix}`,
  );
});
