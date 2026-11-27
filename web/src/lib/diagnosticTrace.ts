export const DIAGNOSTIC_TRACE_SCHEMA = "LEAFEN_DIAGNOSTIC_TRACE_V1";

const DIAGNOSTIC_REFERENCE = /^LF-[A-F0-9]{12}-[A-F0-9]{6}$/;
let fallbackSequence = 0;

function hex(bytes: Uint8Array) {
  return Array.from(bytes, (value) => value.toString(16).padStart(2, "0"))
    .join("")
    .toUpperCase();
}

function fallbackReference() {
  fallbackSequence = (fallbackSequence + 1) % 0x1000000;
  let random = 0;
  try {
    random = Math.floor(Math.random() * 0x1000000);
  } catch {
    random = fallbackSequence;
  }
  let instant = 0;
  try {
    instant = Date.now();
  } catch {
    instant = fallbackSequence;
  }
  const prefix = Math.max(0, instant).toString(16).slice(-12).padStart(12, "0");
  const suffix = (random ^ fallbackSequence).toString(16).slice(-6).padStart(6, "0");
  return `LF-${prefix}-${suffix}`.toUpperCase();
}

export function createDiagnosticReference() {
  try {
    const bytes = new Uint8Array(9);
    globalThis.crypto.getRandomValues(bytes);
    const value = hex(bytes);
    return `LF-${value.slice(0, 12)}-${value.slice(12)}`;
  } catch {
    // Diagnostic identity creation must never replace the observed failure.
    return fallbackReference();
  }
}

export function isDiagnosticReference(value: unknown): value is string {
  return typeof value === "string" && DIAGNOSTIC_REFERENCE.test(value);
}

export function safeDiagnosticReference(value: unknown) {
  return isDiagnosticReference(value) ? value : createDiagnosticReference();
}

export function messageWithDiagnosticReference(
  message: string,
  reference: string,
  label = "Reference",
) {
  return `${message}\n${label}: ${reference}`;
}
