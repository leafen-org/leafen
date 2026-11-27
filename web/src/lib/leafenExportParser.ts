import {
  isLeafenExportDocument,
  type LeafenExportDocument,
} from "./leafenExportContract";

export type LeafenExportParserBounds = {
  maxTextCharacters: number;
};

export type LeafenExportParseStage = "text" | "json" | "document";

export type LeafenExportParseErrorCode =
  | "LEAFEN_EXPORT_BOUND_EXCEEDED"
  | "LEAFEN_EXPORT_JSON_INVALID"
  | "LEAFEN_EXPORT_DOCUMENT_INVALID";

export type LeafenExportParseError = Error & {
  code: LeafenExportParseErrorCode;
  stage: LeafenExportParseStage;
};

function parseError(
  code: LeafenExportParseErrorCode,
  stage: LeafenExportParseStage,
  message: string,
): LeafenExportParseError {
  const error = Object.assign(new Error(message), { code, stage });
  error.name = "LeafenExportParseError";
  return error;
}

function assertParserBounds(bounds: LeafenExportParserBounds) {
  for (const [name, value] of Object.entries(bounds)) {
    if (!Number.isSafeInteger(value) || value <= 0) {
      throw new RangeError(`${name} must be a positive safe integer.`);
    }
  }
}

export function parseLeafenExportDocument(
  text: string,
  bounds: LeafenExportParserBounds,
): LeafenExportDocument {
  assertParserBounds(bounds);
  if (text.length > bounds.maxTextCharacters) {
    throw parseError(
      "LEAFEN_EXPORT_BOUND_EXCEEDED",
      "text",
      "Leafen export input exceeds its caller-owned text bound.",
    );
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    throw parseError(
      "LEAFEN_EXPORT_JSON_INVALID",
      "json",
      "Leafen export input is not valid JSON.",
    );
  }

  if (!isLeafenExportDocument(parsed)) {
    throw parseError(
      "LEAFEN_EXPORT_DOCUMENT_INVALID",
      "document",
      "Leafen export input does not match the supported document version.",
    );
  }
  return parsed;
}
