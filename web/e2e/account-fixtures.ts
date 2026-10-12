import { expect, type Page, type APIRequestContext } from "@playwright/test";

export async function loginAPI(
  request: APIRequestContext,
  data: { tenant: string; email: string; password: string },
) {
  let response = await request.post("/api/v1/auth/login", { data });
  if (response.status() === 429) {
    const seconds = Number(response.headers()["retry-after"]);
    expect(seconds).toBeGreaterThan(0);
    expect(seconds).toBeLessThanOrEqual(60);
    await new Promise((resolve) => setTimeout(resolve, seconds * 1000));
    response = await request.post("/api/v1/auth/login", { data });
  }
  return response;
}

export const permanentRootPassword =
  process.env.E2E_ADMIN_PASSWORD === "admin"
    ? "e2e-personal-administrator-password"
    : process.env.E2E_ADMIN_PASSWORD!;
export const personalPassword = (temporary: string) => `${temporary}-personal`;

export async function changeTemporaryPassword(
  page: Page,
  temporary: string,
  next: string,
) {
  await expect(
    page.getByRole("heading", { name: "Moje konto", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Wymagana zmiana hasła", { exact: true }),
  ).toBeVisible();
  await page.getByLabel("Dotychczasowe hasło", { exact: true }).fill(temporary);
  await page.getByLabel("Nowe hasło", { exact: true }).fill(next);
  await page.getByLabel("Potwierdź nowe hasło", { exact: true }).fill(next);
  await page.getByRole("button", { name: "Zmień hasło", exact: true }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "Hasło zmienione" }),
  ).toBeVisible();
  await expect(
    page.getByText("Wymagana zmiana hasła", { exact: true }),
  ).toHaveCount(0);
}
