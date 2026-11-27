import { throwLeafenApiError } from "@/apiTransport";
import type { NativeFormApi } from "@/formApi";
import {
  formBounds,
  formErrorCodes,
  type FormConfigureRequest,
  type FormConfigureResponse,
  type FormDefinitionResponse,
  type FormSubmitResponse,
} from "@/formWire.generated";
import { PROPERTY_OPTION_CAPACITY } from "@/components/database/queryBudget";
import type {
  FormDefinitionResult,
  FormOptionsResult,
  FormSubmitResult,
} from "@/lib/types";
import type {
  FormTransportAuthority,
  PublicFormService,
} from "@/lib/publicFormView";
import type { Session } from "@/types";

export const nativeFormSurfaceBounds = {
  // However many properties the database holds; the questions keep the server's bound.
  maxProperties: Number.MAX_SAFE_INTEGER,
  maxQuestions: Number.MAX_SAFE_INTEGER,
  maxOptionsPerProperty: PROPERTY_OPTION_CAPACITY,
  // As many choices as the windows a respondent scrolls to bring.
  maxReferenceOptionsPerProperty: Number.MAX_SAFE_INTEGER,
  maxSelectionsPerProperty: Number.MAX_SAFE_INTEGER,
  maxReferenceSelections: Number.MAX_SAFE_INTEGER,
  maxScalarCharacters: formBounds.scalarBytes,
  maxTotalCharacters: formBounds.requestBytes,
} as const;

type FormConfigureInput = Omit<FormConfigureRequest, "action">;

export interface NativeFormService extends PublicFormService {
  configure(input: FormConfigureInput): Promise<FormConfigureResponse>;
  /**
   * False while the page's views are a copy kept on this device or held from
   * before, not yet the server's answer; a form isn't taken for a new one then.
   */
  viewsRead?: boolean;
}

// A submission answers once, with the row it made or had made for the same
// request.
function submission(result: FormSubmitResponse): FormSubmitResult {
  if (!result.rowId || !result.confirmation) {
    return throwLeafenApiError(
      formErrorCodes.failed,
      502,
      "Leafen form submission returned no row.",
      { stage: "form.submit" },
    );
  }
  return {
    rowId: result.rowId,
    replayed: result.replayed,
    confirmation: result.confirmation,
  };
}

export function createNativeFormService(
  api: NativeFormApi,
  currentSession: () => Session | null,
): NativeFormService {
  const authoritySession = (authority: FormTransportAuthority) => (
    authority === "member" ? currentSession() : undefined
  );

  return {
    configure: (input) => api.configureForm(currentSession(), input),
    async getDefinition(token, authority) {
      const result: FormDefinitionResponse = await api.getFormDefinition(
        { token },
        authoritySession(authority),
      );
      return result as FormDefinitionResult;
    },
    async getOptions({ token, propertyId, revision, after, query, pageFilter }) {
      const result = await api.getFormOptions(currentSession(), {
        token,
        propertyId,
        revision,
        ...(after ? { cursor: after } : {}),
        ...(query ? { query } : {}),
        ...(pageFilter ? { pageFilter } : {}),
      });
      return {
        propertyId: result.propertyId,
        type: result.type,
        options: result.options,
        hasMore: result.hasMore,
        after: result.nextCursor,
        taskDatabase: result.taskDatabase === true,
        targetTitle: result.targetTitle ?? "",
        ...(result.targetIcon && result.targetIconType ? { targetIcon: result.targetIcon, targetIconType: result.targetIconType } : {}),
      } as FormOptionsResult;
    },
    async restoreAuthSession() {
      return currentSession()?.user.id ?? "";
    },
    respondent() {
      const user = currentSession()?.user;
      return user ? { id: user.id, name: user.name } : null;
    },
    async submit(input, authority) {
      return submission(await api.submitForm(input, authoritySession(authority)));
    },
    async copyResponse(input) {
      return (await api.copyFormResponse(input, currentSession())).sent === true;
    },
    uploadFile: (input, authority) => api.uploadFormFile(input, authoritySession(authority)),
    searchPlaces: (input, authority, signal) => api.searchFormPlaces(input, authoritySession(authority), signal),
  };
}
