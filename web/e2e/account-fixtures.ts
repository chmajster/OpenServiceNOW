import { expect, type Page } from "@playwright/test";

export const permanentRootPassword = process.env.E2E_ADMIN_PASSWORD === "admin" ? "e2e-personal-administrator-password" : process.env.E2E_ADMIN_PASSWORD!;
export const personalPassword = (temporary: string) => `${temporary}-personal`;

export async function changeTemporaryPassword(page: Page, temporary: string, next: string) {
  await expect(page.getByRole("heading", { name: "Moje konto", exact: true })).toBeVisible();
  await expect(page.getByText("Wymagana zmiana hasła", { exact: true })).toBeVisible();
  await page.getByLabel("Dotychczasowe hasło", { exact: true }).fill(temporary);
  await page.getByLabel("Nowe hasło", { exact: true }).fill(next);
  await page.getByLabel("Potwierdź nowe hasło", { exact: true }).fill(next);
  await page.getByRole("button", { name: "Zmień hasło", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Hasło zmienione" })).toBeVisible();
  await expect(page.getByText("Wymagana zmiana hasła", { exact: true })).toHaveCount(0);
}
