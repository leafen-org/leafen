import bugReportForm from "../../../internal/app/bug_report_form.json";
import packageMetadata from "../../package.json";

import {
  createDiagnosticReference,
  isDiagnosticReference,
} from "./diagnosticTrace";
import { PACKAGED_ORIGINS } from "./packagedOrigins";

// The repository's bug form (.github/ISSUE_TEMPLATE/bug_report.yml), where bug
// reports go (CONTRIBUTING.md), as the server's bug_report_form.json names it
// for an agent's report (leafen_bug_report) too. Its fields are filled by
// their ids with what the app knows safely — the version, the system and
// client, the error's reference, class and route pattern — never page
// content, addresses or anything personal; nothing is sent until the person
// submits the form.
const REPORT_URL = bugReportForm.newIssue;
const REPORT_FORM = bugReportForm.form;
const ERROR_CLASSES = new Set([
  "render-crash",
  "site-not-found",
  "site-rate-limited",
  "site-offline",
  "site-unavailable",
  "site-outside-graph",
]);
const SAFE_VERSION = /^[0-9A-Za-z][0-9A-Za-z.+-]{0,63}$/;

export type ErrorReportClass =
  | "render-crash"
  | "site-not-found"
  | "site-rate-limited"
  | "site-offline"
  | "site-unavailable"
  | "site-outside-graph";

export interface ErrorReportInput {
  errorClass: ErrorReportClass;
  pathname?: unknown;
  reference?: unknown;
  status?: unknown;
  userAgent?: unknown;
}

function safeVersion(value: unknown) {
  return typeof value === "string" && SAFE_VERSION.test(value)
    ? value
    : "unknown";
}

function routePattern(value: unknown) {
  if (typeof value !== "string") return "other";
  const pathname = value.split(/[?#]/, 1)[0] ?? "";
  if (pathname === "/") return "/";
  if (pathname === "/site" || pathname.startsWith("/site/")) return "/site/:slug";
  if (pathname === "/p" || pathname.startsWith("/p/")) return "/p/:pageId";
  if (pathname === "/share" || pathname.startsWith("/share/")) return "/share/:token";
  if (pathname === "/form" || pathname.startsWith("/form/")) return "/form/:token";
  return "other";
}

function browserFamily(value: unknown) {
  const userAgent = typeof value === "string" ? value : "";
  if (/\bEdg(?:e|A|iOS)?\//i.test(userAgent)) return "Edge";
  if (/\b(?:Chrome|CriOS)\//i.test(userAgent)) return "Chrome";
  if (/\bFirefox\//i.test(userAgent)) return "Firefox";
  if (/\bSafari\//i.test(userAgent) && /\bVersion\//i.test(userAgent)) return "Safari";
  return "Other";
}

function osFamily(value: unknown) {
  const userAgent = typeof value === "string" ? value : "";
  if (/\bAndroid\b/i.test(userAgent)) return "Android";
  if (/\b(?:iPhone|iPad|iPod)\b/i.test(userAgent)) return "iOS";
  if (/\bWindows\b/i.test(userAgent)) return "Windows";
  if (/\b(?:Macintosh|Mac OS X)\b/i.test(userAgent)) return "macOS";
  if (/\bLinux\b/i.test(userAgent)) return "Linux";
  return "Other";
}

// The app (desktop or phone) or the browser this page runs in.
function clientKind(value: unknown) {
  const userAgent = typeof value === "string" ? value : "";
  const packaged = typeof window !== "undefined" && PACKAGED_ORIGINS.includes(window.location.origin);
  if (packaged) return /\b(?:Android|iPhone|iPad)\b/i.test(userAgent) ? "phone app" : "desktop app";
  return `${browserFamily(userAgent)} browser`;
}

function safeStatus(value: unknown) {
  const status = typeof value === "number" ? value : Number.NaN;
  return Number.isInteger(status) && status >= 400 && status <= 599
    ? status
    : null;
}

export function errorStatus(error: unknown) {
  if (!error || typeof error !== "object") return null;
  const record = error as { status?: unknown; code?: unknown };
  return safeStatus(record.status) ?? safeStatus(record.code);
}

export function createErrorReference() {
  return createDiagnosticReference();
}

export function errorReportUrl(input: ErrorReportInput) {
  const errorClass = ERROR_CLASSES.has(input.errorClass)
    ? input.errorClass
    : "render-crash";
  const reference = isDiagnosticReference(input.reference)
    ? input.reference
    : "unavailable";
  const status = safeStatus(input.status);
  const details = [
    `Reference: ${reference}`,
    `Error class: ${errorClass}`,
    `Route pattern: ${routePattern(input.pathname)}`,
    ...(status ? [`Status: ${status}`] : []),
  ];
  const url = new URL(REPORT_URL);
  url.searchParams.set("template", REPORT_FORM);
  url.searchParams.set("version", safeVersion(packageMetadata.version));
  url.searchParams.set("system", `${osFamily(input.userAgent)} · ${clientKind(input.userAgent)}`);
  url.searchParams.set("details", details.join("\n"));
  return url.toString();
}
