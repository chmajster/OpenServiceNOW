export type Notification = {
  id: string;
  incident_id: string;
  source_event_id: string;
  kind: string;
  summary: string;
  occurred_at: string;
  read_at?: string;
};
export type NotificationPage = {
  notifications: Notification[];
  unread: string;
  next_before_id?: string;
};
export type Preference = {
  in_app_enabled: boolean;
  smtp_enabled: boolean;
  assignments: boolean;
  replies: boolean;
  state_changes: boolean;
  resolutions: boolean;
  version: string;
};
export type SMTPConfig = {
  provider_id: string;
  enabled: boolean;
  host: string;
  port: number;
  tls_mode: string;
  username: string;
  from_address: string;
  has_password: boolean;
  encryption_ready: boolean;
  insecure_allowed: boolean;
  version: string;
};
export type MailDelivery = {
  id: string;
  user_id: string;
  notification_id?: string;
  provider_id: string;
  status: string;
  attempt_count: number;
  version: string;
  error_code: string;
  created_at: string;
  finished_at?: string;
};
export type EventDelivery = {
  id: string;
  event_id: string;
  consumer_id: string;
  status: string;
  attempt_count: number;
  max_attempts: number;
  last_error: string;
  created_at: string;
  finished_at?: string;
};
export type Attempt = {
  attempt_number: number;
  status: string;
  error_code: string;
  started_at: string;
  finished_at?: string;
};
export const deliveryLabels: Record<string, string> = {
  PENDING: "Oczekuje",
  QUEUED: "W kolejce",
  DELIVERING: "Przetwarzanie",
  SENDING: "Wysyłanie",
  DELIVERED: "Dostarczono",
  FAILED: "Błąd",
  NOT_CONFIGURED: "Kanał nieskonfigurowany",
  SKIPPED: "Pominięto",
  UNKNOWN: "Wynik niepotwierdzony",
};
export const errorLabels: Record<string, string> = {
  SMTP_NOT_CONFIGURED: "Kanał SMTP jest wyłączony lub nieskonfigurowany",
  SMTP_CONNECT_FAILED: "Nie udało się połączyć z serwerem",
  SMTP_TLS_FAILED: "Nie udało się zestawić bezpiecznego połączenia",
  SMTP_AUTH_FAILED: "Serwer odrzucił uwierzytelnienie",
  SMTP_SENDER_REJECTED: "Serwer odrzucił nadawcę",
  SMTP_RECIPIENT_REJECTED: "Serwer odrzucił odbiorcę",
  SMTP_DATA_REJECTED: "Serwer odrzucił wiadomość",
  SMTP_RESULT_UNKNOWN:
    "Nie potwierdzono przyjęcia wiadomości; ponowienie może utworzyć duplikat",
  SMTP_KEY_UNAVAILABLE: "Klucz szyfrowania jest niedostępny lub niezgodny",
  SMTP_PROTOCOL_FAILED: "Błąd protokołu SMTP",
  CONSUMER_FAILED: "Odbiorca nie zakończył przetwarzania",
  LEASE_EXHAUSTED: "Wyczerpano próby po utracie procesu",
  JOB_EXHAUSTED: "Zadanie wyczerpało próby",
  LEASE_LOST: "Proces utracił lease",
  SKIPPED: "Brak bieżącego dostępu lub wyłączona preferencja",
  NOT_CONFIGURED: "Kanał SMTP jest wyłączony lub nieskonfigurowany",
};
