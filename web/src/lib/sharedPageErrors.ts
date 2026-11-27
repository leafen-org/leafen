export type SharedPageErrorKind = "not-found" | "rate-limited" | "offline" | "unavailable";

function safeProperty(error: unknown, property: string): unknown {
  if ((typeof error !== "object" || error === null) && typeof error !== "function") {
    return undefined;
  }
  try {
    return Reflect.get(error, property);
  } catch {
    return undefined;
  }
}

export function sharedPageErrorKind(error: unknown): SharedPageErrorKind {
  const numericStatus = [safeProperty(error, "status"), safeProperty(error, "code")]
    .find((value) => typeof value === "number");
  const semanticCode = ["slug", "errorCode", "error_code", "code"]
    .map((property) => safeProperty(error, property))
    .find((value) => typeof value === "string");
  const normalizedCode = String(semanticCode ?? "").toLowerCase();

  // Public share endpoints intentionally collapse missing/expired/forbidden
  // resources so the UI never becomes an existence oracle.
  if (
    [401, 403, 404, 410].includes(Number(numericStatus)) ||
    /not.?found|expired|revoked|unpublished|invalid.?token|access.?denied|forbidden/.test(normalizedCode)
  ) {
    return "not-found";
  }
  if (Number(numericStatus) === 429 || /rate.?limit/.test(normalizedCode)) {
    return "rate-limited";
  }
  if (
    error instanceof TypeError || /transport|network|offline/.test(normalizedCode)
  ) {
    return "offline";
  }
  return "unavailable";
}
