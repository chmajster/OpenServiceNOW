import type { ApiErrorBody } from "./types";

const CSRF_COOKIE_NAME = "opensnow_csrf";
const errorMessages: Record<string, string> = {
  UNAUTHENTICATED: "Sesja wygasła. Zaloguj się ponownie.",
  FORBIDDEN: "Nie masz uprawnienia do tej operacji.",
  PLATFORM_ADMIN_REQUIRED:
    "Ta operacja wymaga uprawnień administratora platformy.",
  LAST_PLATFORM_ADMIN:
    "Organizacja musi zachować co najmniej jednego aktywnego administratora platformy.",
  RBAC_RESOURCE_NOT_FOUND: "Nie znaleziono roli lub grupy w tej organizacji.",
  INVALID_RBAC_INPUT:
    "Nieprawidłowe dane roli, grupy lub przypisania. Sprawdź formularz. Ról systemowych nie można edytować ani usuwać sobie uprawnień administratora.",
  INVALID_USER_INPUT:
    "Nieprawidłowe dane użytkownika. Sprawdź adres e-mail, nazwę i długość hasła; nie można wyłączyć własnego członkostwa.",
  USER_NOT_FOUND: "Nie znaleziono użytkownika w tej organizacji.",
  CSRF_VALIDATION_FAILED:
    "Token zabezpieczający wygasł. Odśwież sesję i spróbuj ponownie.",
  INVALID_AUDIT_FILTER:
    "Nieprawidłowy filtr audytu. Sprawdź daty, identyfikator aktora i długość wpisanych wartości.",
  INVALID_PAGINATION: "Nieprawidłowy filtr lub kursor listy.",
};

let sessionRevision = 0;
const sessionListeners = new Set<() => void>();
export function advanceSessionRevision() {
  sessionRevision += 1;
}
export function onSessionExpired(listener: () => void) {
  sessionListeners.add(listener);
  return () => {
    sessionListeners.delete(listener);
  };
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly requestId?: string;

  constructor(
    status: number,
    code: string,
    message: string,
    requestId?: string,
  ) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.requestId = requestId;
  }
}

export function readCookie(name: string): string | undefined {
  const prefix = `${encodeURIComponent(name)}=`;
  for (const part of document.cookie.split(";")) {
    const value = part.trim();
    if (value.startsWith(prefix)) {
      return decodeURIComponent(value.slice(prefix.length));
    }
  }
  return undefined;
}

function isUnsafeMethod(method: string): boolean {
  return !["GET", "HEAD", "OPTIONS"].includes(method.toUpperCase());
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const revision = sessionRevision;
  const method = (init.method ?? "GET").toUpperCase();
  const headers = new Headers(init.headers);

  if (init.body !== undefined && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  if (isUnsafeMethod(method)) {
    const csrf = readCookie(CSRF_COOKIE_NAME);
    if (csrf) {
      headers.set("X-CSRF-Token", csrf);
    }
  }

  const response = await fetch(path, {
    ...init,
    method,
    headers,
    credentials: "include",
  });

  if (init.signal?.aborted || revision !== sessionRevision)
    throw new DOMException("Nieaktualna sesja", "AbortError");
  if (
    response.status === 401 &&
    path !== "/api/v1/auth/login" &&
    !path.startsWith("/api/v1/setup")
  ) {
    advanceSessionRevision();
    sessionListeners.forEach((listener) => listener());
  }
  const responseRevision = sessionRevision;

  if (response.status === 204) {
    return undefined as T;
  }

  const contentType = response.headers.get("Content-Type") ?? "";
  const payload = contentType.includes("application/json")
    ? await response.json()
    : undefined;
  if (init.signal?.aborted || responseRevision !== sessionRevision)
    throw new DOMException("Nieaktualna sesja", "AbortError");

  if (!response.ok) {
    const body = payload as ApiErrorBody | undefined;
    throw new ApiError(
      response.status,
      body?.error?.code ?? "HTTP_ERROR",
      errorMessages[body?.error?.code ?? ""] ??
        body?.error?.message ??
        `Żądanie zakończyło się błędem HTTP ${response.status}`,
      body?.error?.request_id,
    );
  }

  return payload as T;
}

export function jsonBody(value: unknown): string {
  return JSON.stringify(value);
}
