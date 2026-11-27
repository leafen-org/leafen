import {
  productErrorPresentation,
  productErrorMessage,
  type ProductErrorContext,
} from "@/lib/productErrorPresentation";
import type { PlaceSearchResponse } from "@/administratorWire.generated";
import type {
  FormDefinitionResult,
  FormOptionsResult,
  FormSubmitResult,
} from "@/lib/types";

export type FormTransportAuthority = "public" | "member";

export interface PublicFormService {
  getDefinition(
    token: string,
    authority: FormTransportAuthority,
  ): Promise<FormDefinitionResult>;
  getOptions(input: {
    token: string;
    propertyId: string;
    revision: string;
    after?: string;
    /** What the respondent typed in the answer's search. */
    query?: string;
    pageFilter?: "all" | "verified" | "archived";
  }): Promise<FormOptionsResult>;
  restoreAuthSession(): Promise<string>;
  /** The signed-in person a member's response is submitted as. */
  respondent(): { id: string; name: string } | null;
  submit(input: {
    token: string;
    revision: string;
    requestId: string;
    answers: Record<string, unknown>;
  }, authority: FormTransportAuthority): Promise<FormSubmitResult>;
  /** Mails the signed-in respondent a copy of the response their submit made. */
  copyResponse(input: { token: string; requestId: string }): Promise<boolean>;
  /** Sends a file for a file question; its answer names it by the id. */
  uploadFile?(input: { token: string; propertyId: string; requestId: string; file: File }, authority: FormTransportAuthority):
    Promise<{ fileId: string; name: string; size: number; contentType: string }>;
  /** Searches places by name for a place question, where the server has a place search provider. */
  searchPlaces?(input: { token: string; propertyId: string; query: string }, authority: FormTransportAuthority,
    signal?: AbortSignal): Promise<PlaceSearchResponse>;
}

export const PUBLIC_FORM_FAILURE_STAGES = [
  "definition.read",
  "auth.restore",
  "options.read",
  "configuration.write",
  "response.submit",
  "response.copy",
] as const;

export type PublicFormFailureStage = typeof PUBLIC_FORM_FAILURE_STAGES[number];

const publicFormFailureContexts = {
  "definition.read": "publicForm.definitionRead",
  "auth.restore": "publicForm.authRestore",
  "options.read": "publicForm.optionsRead",
  "configuration.write": "publicForm.configurationWrite",
  "response.submit": "publicForm.responseSubmit",
  "response.copy": "publicForm.responseSubmit",
} as const satisfies Record<PublicFormFailureStage, ProductErrorContext>;

export function publicFormErrorStatus(error: unknown) {
  if (!error || typeof error !== "object") return null;
  try {
    const status = Reflect.get(error, "status");
    const value = status ?? Reflect.get(error, "code");
    return typeof value === "number" ? value : null;
  } catch {
    return null;
  }
}

export function publicFormFailureReference(
  error: unknown,
  stage: PublicFormFailureStage,
) {
  return productErrorPresentation(error, publicFormFailureContexts[stage]).reference;
}

export function publicFormFailureMessage(
  error: unknown,
  stage: PublicFormFailureStage,
) {
  return productErrorMessage(error, publicFormFailureContexts[stage]);
}
