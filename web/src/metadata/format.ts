import type { Locale } from "./contracts";

function settings() {
  if (typeof document === "undefined") return { timeZone: "UTC", dateFormat: "YYYY-MM-DD", numberFormat: "1,234.56" };
  return {
    timeZone: document.documentElement.dataset.timeZone || "UTC",
    dateFormat: document.documentElement.dataset.dateFormat || "YYYY-MM-DD",
    numberFormat: document.documentElement.dataset.numberFormat || "1,234.56",
  };
}

function localeTag(locale: Locale) {
  return locale === "en" ? "en-GB" : "pl-PL";
}

function orderDate(year: string, month: string, day: string, format: string) {
  if (format === "DD.MM.YYYY") return `${day}.${month}.${year}`;
  if (format === "MM/DD/YYYY") return `${month}/${day}/${year}`;
  return `${year}-${month}-${day}`;
}

export function formatDate(value: string, locale: Locale) {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  if (match) return orderDate(match[1]!, match[2]!, match[3]!, settings().dateFormat);
  return formatDateTime(value, locale);
}

export function formatDateTime(value: string | null | undefined, locale: Locale) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const { timeZone, dateFormat } = settings();
  const parts = new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" }).formatToParts(date);
  const part = (type: string) => parts.find((item) => item.type === type)?.value ?? "";
  const datePart = orderDate(part("year"), part("month"), part("day"), dateFormat);
  const timePart = new Intl.DateTimeFormat(localeTag(locale), { timeZone, hour: "2-digit", minute: "2-digit" }).format(date);
  return `${datePart} ${timePart}`;
}

export function formatNumber(value: number, locale: Locale) {
  const format = settings().numberFormat;
  const tag = format === "1.234,56" ? "de-DE" : localeTag(locale);
  return new Intl.NumberFormat(tag, { maximumFractionDigits: 6 }).format(value);
}

export function formatMetadataValue(value: unknown, locale: Locale, type?: string) {
  if (value === null || value === undefined || value === "") return "—";
  if (Array.isArray(value)) return value.map((item) => String(item)).join(", ");
  if (typeof value === "boolean") return value ? (locale === "en" ? "Yes" : "Tak") : (locale === "en" ? "No" : "Nie");
  if (typeof value === "number") return formatNumber(value, locale);
  if (typeof value === "string" && type === "DATE") return formatDate(value, locale);
  if (typeof value === "string" && type === "DATETIME") return formatDateTime(value, locale);
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}
