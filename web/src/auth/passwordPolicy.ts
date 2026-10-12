export function passwordProblem(value: string): string | null {
  if (Array.from(value).length < 12) return "Hasło musi mieć co najmniej 12 znaków Unicode.";
  if (new TextEncoder().encode(value).length > 1024) return "Hasło może mieć najwyżej 1024 bajty UTF-8.";
  return null;
}
