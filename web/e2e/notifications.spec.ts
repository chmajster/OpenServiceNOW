import { expect, test, type APIRequestContext } from "@playwright/test";
import {
  loginAPI,
  permanentRootPassword,
  changeTemporaryPassword,
  personalPassword,
} from "./account-fixtures";

const tenant = process.env.E2E_TENANT ?? "e2e";
async function csrf(request: APIRequestContext) {
  return {
    "X-CSRF-Token": (await request.storageState()).cookies.find(
      (c) => c.name === "opensnow_csrf",
    )!.value,
  };
}
test("independent incident notifications, preferences, current access and real SMTP queue", async ({
  page,
  request,
  browser,
}, info) => {
  test.setTimeout(180_000);
  expect(
    (
      await loginAPI(request, {
        tenant,
        email: process.env.E2E_ADMIN_EMAIL ?? "admin",
        password: permanentRootPassword,
      })
    ).status(),
  ).toBe(200);
  const headers = await csrf(request),
    scope = `notify-${info.project.name}-${Date.now()}`,
    email = `${scope}@example.test`,
    temporary = "notification-temporary-password";
  const userResponse = await request.post("/api/v1/admin/users/", {
    headers,
    data: { email, display_name: scope, password: temporary },
  });
  expect(userResponse.status()).toBe(201);
  const user = (await userResponse.json()).id as string;
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
  const role = (await roleResponse.json()).id as string;
  expect(
    (
      await request.post(`/api/v1/admin/rbac/users/${user}/roles/${role}`, {
        headers,
      })
    ).status(),
  ).toBe(204);
  const reporterContext = await browser.newContext({
      baseURL: process.env.E2E_BASE_URL ?? "http://127.0.0.1:5173",
      ...(info.project.name === "mobile-chromium"
        ? {
            viewport: { width: 412, height: 915 },
            isMobile: true,
            hasTouch: true,
          }
        : {}),
    }),
    reporter = await reporterContext.newPage();
  let originalConfig: Record<string, unknown> | undefined;
  try {
    expect(
      (
        await loginAPI(reporter.request, { tenant, email, password: temporary })
      ).status(),
    ).toBe(200);
    await reporter.goto("/account");
    await changeTemporaryPassword(
      reporter,
      temporary,
      personalPassword(temporary),
    );
    const reporterHeaders = await csrf(reporter.request);
    const created = await reporter.request.post("/api/v1/incidents/", {
      headers: { ...reporterHeaders, "Idempotency-Key": scope },
      data: {
        title: scope,
        description: "Notification browser proof",
        category: "other",
        subcategory: "other",
        impact: 2,
        urgency: 2,
      },
    });
    expect(created.status()).toBe(201);
    const incident = (await created.json()).id as string;
    await reporter.goto("/notifications?tab=preferences");
    await reporter.getByLabel("Powiadomienia e-mail", { exact: false }).check();
    await reporter
      .getByRole("button", { name: "Zapisz preferencje", exact: true })
      .click();
    await expect(
      reporter.getByText("Zapisano preferencje", { exact: true }),
    ).toBeVisible();
    await reporter.reload();
    await expect(
      reporter.getByLabel("Powiadomienia e-mail", { exact: false }),
    ).toBeChecked();
    const preference = await (
      await reporter.request.get("/api/v1/notifications/preferences")
    ).json();
    await reporter.getByLabel("Zmiany stanu", { exact: true }).uncheck();
    expect(
      (
        await reporter.request.put("/api/v1/notifications/preferences", {
          headers: reporterHeaders,
          data: { ...preference, assignments: false },
        })
      ).status(),
    ).toBe(204);
    await reporter
      .getByRole("button", { name: "Zapisz preferencje", exact: true })
      .click();
    await expect(
      reporter.getByRole("heading", { name: "Konflikt wersji", exact: true }),
    ).toBeVisible();
    await expect(
      reporter.getByLabel("Zmiany stanu", { exact: true }),
    ).not.toBeChecked();
    await reporter
      .getByRole("button", {
        name: "Zachowaj propozycję do ponownego zapisu",
        exact: true,
      })
      .click();
    await reporter
      .getByRole("button", { name: "Zapisz preferencje", exact: true })
      .click();
    await expect(
      reporter.getByText("Zapisano preferencje", { exact: true }),
    ).toBeVisible();
    expect(
      (
        await request.post(`/api/v1/incidents/${incident}/messages`, {
          headers: { ...headers, "Idempotency-Key": `${scope}-public` },
          data: { type: "COMMENT", body: "Public notification reply" },
        })
      ).status(),
    ).toBe(200);
    expect(
      (
        await request.post(`/api/v1/incidents/${incident}/messages`, {
          headers: { ...headers, "Idempotency-Key": `${scope}-private` },
          data: { type: "WORK_NOTE", body: "NEVER-LEAK-NOTIFICATION-NOTE" },
        })
      ).status(),
    ).toBe(200);
    await expect
      .poll(
        async () => {
          const result = await reporter.request.get("/api/v1/notifications/");
          expect(result.status()).toBe(200);
          const body = await result.json();
          expect(JSON.stringify(body)).not.toContain("NEVER-LEAK");
          return body.notifications.length;
        },
        { timeout: 20_000 },
      )
      .toBe(1);
    await reporter.getByRole("button", { name: "Lista", exact: true }).click();
    await expect(
      reporter.getByText("Nowa odpowiedź w incydencie", { exact: true }),
    ).toBeVisible();
    await expect(
      reporter.getByText("Nieprzeczytane: 1", { exact: true }),
    ).toBeVisible();
    const notificationData = await (
      await reporter.request.get("/api/v1/notifications/")
    ).json();
    expect(typeof notificationData.notifications[0].id).toBe("string");
    if (process.env.E2E_BIGINT === "1")
      expect(BigInt(notificationData.notifications[0].id)).toBeGreaterThan(
        9007199254740991n,
      );
    await reporter
      .getByRole("button", { name: "Oznacz jako przeczytane", exact: true })
      .click();
    await expect(
      reporter.getByText("Nieprzeczytane: 0", { exact: true }),
    ).toBeVisible();
    await reporter.screenshot({
      path: info.outputPath("notification-inbox.png"),
      fullPage: true,
    });
    await reporter
      .getByRole("link", { name: `Otwórz incydent #${incident}`, exact: true })
      .click();
    await expect(reporter).toHaveURL(new RegExp(`/incidents/${incident}`));
    await reporter.goto("/notifications?tab=mail");
    await expect(
      reporter.getByRole("cell", {
        name: "Kanał nieskonfigurowany",
        exact: true,
      }),
    ).toBeVisible();
    // Admin UI talks to real SMTP; the dedicated E2E CI service accepts it. Other
    // runtime suites deliberately use a closed port and verify the real failure.
    expect(
      (
        await loginAPI(page.request, {
          tenant,
          email: process.env.E2E_ADMIN_EMAIL ?? "admin",
          password: permanentRootPassword,
        })
      ).status(),
    ).toBe(200);
    originalConfig = await (
      await request.get("/api/v1/admin/notifications/smtp")
    ).json();
    await page.goto("/notification-admin");
    await page
      .getByLabel("Identyfikator dostawcy", { exact: true })
      .fill("browser-smtp-proof");
    await page.getByLabel("Serwer SMTP", { exact: true }).fill("127.0.0.1");
    await page
      .getByLabel("Port", { exact: true })
      .fill(process.env.E2E_SMTP_PORT ?? "1");
    await page
      .getByLabel("Adres nadawcy", { exact: true })
      .fill("sender@example.test");
    if (process.env.E2E_SMTP_PORT)
      await page.getByLabel("Szyfrowanie połączenia").selectOption("NONE");
    await page.getByLabel("Włącz kanał SMTP").check();
    await page
      .getByRole("button", { name: "Zapisz konfigurację", exact: true })
      .click();
    await expect(
      page.getByText("Zapisano konfigurację SMTP", { exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Wyślij próbę na mój adres", exact: true })
      .click();
    await expect(
      page.getByRole("status").filter({ hasText: "została zakolejkowana" }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Dostawy SMTP", exact: true })
      .click();
    const targetStatus = process.env.E2E_SMTP_PORT ? "DELIVERED" : "FAILED";
    await expect
      .poll(
        async () => {
          const result = await request.get("/api/v1/admin/notifications/mail");
          const data = await result.json();
          return data.deliveries.find(
            (d: { user_id: string; notification_id?: string }) =>
              d.user_id !== user && !d.notification_id,
          )?.status;
        },
        { timeout: 30_000 },
      )
      .toBe(targetStatus);
    await page
      .getByRole("button", { name: "Odśwież kolejkę", exact: true })
      .click();
    await expect(
      page
        .getByRole("cell", {
          name: process.env.E2E_SMTP_PORT ? "Dostarczono" : "Błąd",
          exact: true,
        })
        .first(),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Historia prób", exact: true })
      .first()
      .click();
    await expect(
      page.getByRole("heading", { name: /Historia prób dostawy/ }),
    ).toBeVisible();
    await expect(page.getByText(/Próba 1:/)).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: info.outputPath("notification-deliveries.png"),
      fullPage: true,
    });
    if (!process.env.E2E_SMTP_PORT) {
      await page
        .getByRole("button", { name: "Ponów", exact: true })
        .first()
        .click();
      await expect(page.getByText(/Ponowić dostawę/)).toBeVisible();
      await page.getByRole("button", { name: "Anuluj", exact: true }).click();
    }
    await page
      .getByRole("button", { name: "Odbiorcy zdarzeń", exact: true })
      .click();
    await expect(
      page.getByRole("cell", { name: "activity.v1", exact: true }).first(),
    ).toBeVisible();
    await expect(
      page.getByRole("cell", { name: "notifications.v1", exact: true }).first(),
    ).toBeVisible();
    expect(
      (await reporter.request.get("/api/v1/admin/notifications/mail")).status(),
    ).toBe(403);
    expect(
      (
        await request.delete(`/api/v1/admin/rbac/users/${user}/roles/${role}`, {
          headers,
        })
      ).status(),
    ).toBe(204);
    const hidden = await (
      await reporter.request.get("/api/v1/notifications/")
    ).json();
    expect(hidden.notifications).toEqual([]);
    expect(hidden.unread).toBe("0");
    expect(
      (
        await reporter.request.post(
          `/api/v1/notifications/${notificationData.notifications[0].id}/read`,
          { headers: reporterHeaders },
        )
      ).status(),
    ).toBe(404);
    await reporter.goto("/notifications");
    await expect(
      reporter.getByText("Brak powiadomień", { exact: true }),
    ).toBeVisible();
    expect(
      await reporter.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
  } finally {
    if (originalConfig) {
      const fresh = await (
        await request.get("/api/v1/admin/notifications/smtp")
      ).json();
      expect(
        (
          await request.put("/api/v1/admin/notifications/smtp", {
            headers,
            data: {
              provider_id: originalConfig.provider_id || "disabled-local",
              enabled: false,
              host: originalConfig.host || "127.0.0.1",
              port: originalConfig.port || 587,
              tls_mode: originalConfig.tls_mode || "STARTTLS",
              username: originalConfig.username || "",
              from_address:
                originalConfig.from_address || "sender@example.test",
              version: fresh.version,
            },
          })
        ).status(),
      ).toBe(204);
    }
    await reporterContext.close();
  }
});
