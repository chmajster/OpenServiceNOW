import type { ApiErrorBody } from "./types";

const CSRF_COOKIE_NAME = "opensnow_csrf";
const errorMessages: Record<string, string> = {
  ACCOUNT_RESULT_UNKNOWN:
    "Nie potwierdzono wyniku zmiany hasła. Odśwież stan konta lub zaloguj się nowym hasłem. Nie ponawiaj automatycznie.",
  AUTH_BUSY:
    "Weryfikacja haseł jest chwilowo zajęta. Spróbuj ponownie za chwilę.",
  AUTH_RATE_LIMITED: "Zbyt wiele prób. Odczekaj przed kolejną próbą.",
  AUTH_RATE_LIMIT_UNAVAILABLE: "Weryfikacja haseł jest chwilowo niedostępna.",
  ACCOUNT_FAILED: "Nie udało się wykonać operacji konta.",
  PASSWORD_CHANGE_REQUIRED: "Zmień hasło tymczasowe, aby kontynuować.",
  CURRENT_PASSWORD_INVALID: "Dotychczasowe hasło jest nieprawidłowe.",
  ACCOUNT_CONFLICT:
    "Konto zmieniło się w trakcie edycji. Sprawdź aktualny stan przed ponowieniem.",
  ACCOUNT_INVALID:
    "Sprawdź nazwę i hasło: co najmniej 12 znaków Unicode, najwyżej 1024 bajty UTF-8. Nowe hasło musi być inne niż dotychczasowe.",
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
  KNOWLEDGE_CONFLICT:
    "Baza wiedzy zmieniła się. Odczytaj aktualną wersję i rozstrzygnij konflikt.",
  KNOWLEDGE_IDEMPOTENCY_CONFLICT:
    "Ten klucz operacji bazy wiedzy był już użyty z innymi danymi.",
};

let sessionRevision = 0;
const sessionListeners = new Set<() => void>();
const sessionRevisionListeners = new Set<() => void>();
export function advanceSessionRevision() {
  sessionRevision += 1;
  sessionRevisionListeners.forEach((listener) => listener());
}
export function currentSessionRevision() {
  return sessionRevision;
}
export function onSessionRevisionChange(listener: () => void) {
  sessionRevisionListeners.add(listener);
  return () => {
    sessionRevisionListeners.delete(listener);
  };
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

export async function api<T>(
  path: string,
  init: RequestInit = {},
  mode: "json" | "blob" = "json",
  transport: typeof fetch = fetch,
): Promise<T> {
  const revision = sessionRevision;
  const initialCSRF = readCookie(CSRF_COOKIE_NAME);
  const method = (init.method ?? "GET").toUpperCase();
  const headers = new Headers(init.headers);

  if (
    init.body !== undefined &&
    !(init.body instanceof FormData) &&
    !headers.has("Content-Type")
  ) {
    headers.set("Content-Type", "application/json");
  }

  if (isUnsafeMethod(method)) {
    const csrf = readCookie(CSRF_COOKIE_NAME);
    if (csrf) {
      headers.set("X-CSRF-Token", csrf);
    }
  }

  const response = await transport(path, {
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
    if (initialCSRF !== readCookie(CSRF_COOKIE_NAME))
      throw new DOMException("Sesja została odnowiona", "AbortError");
    advanceSessionRevision();
    sessionListeners.forEach((listener) => listener());
  }
  const responseRevision = sessionRevision;

  if (response.status === 204) {
    return undefined as T;
  }

  const contentType = response.headers.get("Content-Type") ?? "";
  const payload =
    response.ok && mode === "blob"
      ? await response.blob()
      : contentType.includes("application/json")
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

// XHR supplies real browser upload progress; session/CSRF/error handling remains
// in the same API pipeline as JSON and file downloads. Browser supplies boundary.
export function upload<T>(
  path: string,
  form: FormData,
  key: string,
  progress: (percent: number) => void,
  signal?: AbortSignal,
) {
  const transport: typeof fetch = (_path, init) =>
    new Promise<Response>((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      const abort = () => xhr.abort();
      const clean = () => signal?.removeEventListener("abort", abort);
      xhr.open("POST", path);
      xhr.withCredentials = true;
      xhr.responseType = "blob";
      new Headers(init?.headers).forEach((value, name) =>
        xhr.setRequestHeader(name, value),
      );
      xhr.upload.onprogress = (event) => {
        if (event.lengthComputable)
          progress(Math.round((event.loaded / event.total) * 100));
      };
      xhr.onload = () => {
        clean();
        const headers = new Headers();
        xhr
          .getAllResponseHeaders()
          .trim()
          .split(/[\r\n]+/)
          .forEach((line) => {
            const at = line.indexOf(":");
            if (at > 0)
              headers.append(line.slice(0, at), line.slice(at + 1).trim());
          });
        resolve(new Response(xhr.response, { status: xhr.status, headers }));
      };
      xhr.onerror = () => {
        clean();
        reject(
          new Error(
            "Nie potwierdzono przesłania pliku. Sprawdź listę lub ponów z zachowanym kluczem.",
          ),
        );
      };
      xhr.onabort = () => {
        clean();
        reject(new DOMException("Przesyłanie przerwane", "AbortError"));
      };
      signal?.addEventListener("abort", abort, { once: true });
      if (signal?.aborted) {
        clean();
        reject(new DOMException("Przesyłanie przerwane", "AbortError"));
        return;
      }
      xhr.send(form);
    });
  return api<T>(
    path,
    { method: "POST", headers: { "Idempotency-Key": key }, body: form, signal },
    "json",
    transport,
  );
}

export async function download(
  path: string,
  name: string,
  signal?: AbortSignal,
) {
  const blob = await api<Blob>(path, { signal }, "blob");
  const url = URL.createObjectURL(blob),
    link = document.createElement("a");
  link.href = url;
  link.download = name;
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

export function jsonBody(value: unknown): string {
  return JSON.stringify(value);
}
