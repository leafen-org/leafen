import type {
  PlacePropertyValue,
  VerificationPropertyValue,
} from "@/lib/types";
import { formatDate, formatNotionTimestamp } from "./dateUtils";

function recordValue(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
}

function optionalText(value: unknown) {
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

export function placePropertyValue(value: unknown): PlacePropertyValue | null {
  const record = recordValue(value);
  if (!record || typeof record.lat !== "number" || !Number.isFinite(record.lat)) return null;
  if (typeof record.lon !== "number" || !Number.isFinite(record.lon)) return null;
  return {
    lat: record.lat,
    lon: record.lon,
    ...(record.name === null || typeof record.name === "string" ? { name: record.name } : {}),
    ...(record.address === null || typeof record.address === "string"
      ? { address: record.address }
      : {}),
    ...(record.aws_place_id === null || typeof record.aws_place_id === "string"
      ? { aws_place_id: record.aws_place_id }
      : {}),
    ...(record.google_place_id === null || typeof record.google_place_id === "string"
      ? { google_place_id: record.google_place_id }
      : {}),
  };
}

function namedLocationSummary(value: unknown) {
  const record = recordValue(value);
  if (!record) return typeof value === "string" ? value.trim() : "";
  const name = optionalText(record.name);
  const address = optionalText(record.address);
  if (name && address && name !== address) return `${name} · ${address}`;
  if (name || address) return name ?? address ?? "";
  if (
    typeof record.lat === "number" && Number.isFinite(record.lat)
    && typeof record.lon === "number" && Number.isFinite(record.lon)
  ) {
    return `${record.lat}, ${record.lon}`;
  }
  return "";
}

// A place reads as Notion's wherever its value shows — a table cell, a row
// page, a board, gallery or calendar card, a list row, a timeline bar (seen
// 2026-11-12): its name alone, else its address, else its point to two
// places ("37.50, 127.00"). One map pin's label reads the same.
export function placePropertySummary(value: unknown) {
  const record = recordValue(value);
  if (!record) return typeof value === "string" ? value.trim() : "";
  const named = optionalText(record.name) ?? optionalText(record.address);
  if (named) return named;
  const place = placePropertyValue(record);
  return place ? `${place.lat.toFixed(2)}, ${place.lon.toFixed(2)}` : "";
}

export function verificationPropertyValue(value: unknown): VerificationPropertyValue | null {
  const record = recordValue(value);
  if (!record || !["verified", "unverified", "expired"].includes(String(record.state))) {
    return null;
  }
  const state = record.state as VerificationPropertyValue["state"];
  const date = record.date === null || record.date === undefined
    ? record.date
    : recordValue(record.date);
  if (date && typeof date.start !== "string") return null;
  return {
    state,
    ...(date !== undefined ? { date: date as VerificationPropertyValue["date"] } : {}),
    ...(record.verified_by !== undefined ? { verified_by: record.verified_by } : {}),
  } as VerificationPropertyValue;
}

// An unverified page's verification is empty, as Notion's wikis show it
// (seen 2026-10-15).
export function verificationPropertySummary(
  value: unknown,
  labels: { verified: string; expired: string; verifiedUntil?: (date: string) => string },
) {
  const verification = verificationPropertyValue(value);
  if (!verification || verification.state === "unverified") return "";
  const label = labels[verification.state];
  const end = verification.date && typeof verification.date === "object" ? verification.date.end : null;
  // One verified indefinitely says so alone, "인증 완료" (Notion, seen 2026-11-11).
  if (verification.state === "verified" && !end) return label;
  // A verification that runs out says until when, "2026년 11월 10일까지 인증 유효", as Notion's (seen 2026-11-03).
  if (verification.state === "verified" && end && labels.verifiedUntil) {
    // The day alone, on the reader's calendar.
    const day = new Date(end);
    if (!Number.isNaN(day.getTime())) {
      const key = `${day.getFullYear()}-${String(day.getMonth() + 1).padStart(2, "0")}-${String(day.getDate()).padStart(2, "0")}`;
      return labels.verifiedUntil(formatDate(key));
    }
  }
  const date = verification.date && typeof verification.date === "object"
    ? verification.date.end ?? verification.date.start
    : null;
  const formattedDate = date ? formatNotionTimestamp(date) : "";
  return formattedDate ? `${label} · ${formattedDate}` : label;
}

export function runtimeOwnedPropertySummary(
  type: "location" | "last_visited_time",
  value: unknown,
) {
  if (type === "location") return namedLocationSummary(value);
  return formatNotionTimestamp(value);
}
