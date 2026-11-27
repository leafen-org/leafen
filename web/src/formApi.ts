import type { Session } from "./types";
import type { PlaceSearchResponse } from "./administratorWire.generated";
import { sha256Hex } from "./lib/browserCrypto";
import { fileTransferBounds, type FileUploadStateResponse } from "./fileTransferWire.generated";
import {
  formActions,
  formOperations,
  type FormConfigureRequest,
  type FormConfigureResponse,
  type FormDefinitionRequest,
  type FormDefinitionResponse,
  type FormOptionsRequest,
  type FormOptionsResponse,
  type FormResponseCopyRequest,
  type FormResponseCopyResponse,
  type FormSubmitRequest,
  type FormSubmitResponse,
} from "./formWire.generated";

type JsonRequest = <T>(
  path: string,
  init?: RequestInit,
  session?: Session | null,
) => Promise<T>;

export function createFormApi(request: JsonRequest) {
  return {
    configureForm: (
      session: Session | null | undefined,
      input: Omit<FormConfigureRequest, "action">,
    ) => request<FormConfigureResponse>(
      formOperations.configure.path,
      {
        method: formOperations.configure.method,
        body: JSON.stringify({ ...input, action: formActions.configure }),
      },
      session,
    ),
    getFormDefinition: (
      input: Omit<FormDefinitionRequest, "action">,
      session?: Session | null,
    ) => request<FormDefinitionResponse>(
      formOperations.definition.path,
      {
        method: formOperations.definition.method,
        body: JSON.stringify({ ...input, action: formActions.definition }),
      },
      session,
    ),
    getFormOptions: (
      session: Session | null | undefined,
      input: Omit<FormOptionsRequest, "action">,
    ) => request<FormOptionsResponse>(
      formOperations.options.path,
      {
        method: formOperations.options.method,
        body: JSON.stringify({ ...input, action: formActions.options }),
      },
      session,
    ),
    submitForm: (
      input: Omit<FormSubmitRequest, "action">,
      session?: Session | null,
    ) => request<FormSubmitResponse>(
      formOperations.submit.path,
      {
        method: formOperations.submit.method,
        body: JSON.stringify({ ...input, action: formActions.submit }),
      },
      session,
    ),
    // A file for a form's file question, sent on its own before the answer names it (form_file_uploads.go);
    // one over a request's size goes up in parts behind the same form checks (form_file_parts.go).
    uploadFormFile: async (
      input: { token: string; propertyId: string; requestId: string; file: File },
      session?: Session | null,
    ) => {
      type SentFile = { fileId: string; name: string; size: number; contentType: string };
      const files = `${formOperations.submit.path}/files`;
      if (input.file.size <= fileTransferBounds.directBytes) {
        const body = new FormData();
        body.set("token", input.token);
        body.set("propertyId", input.propertyId);
        body.set("requestId", input.requestId);
        body.set("file", input.file, input.file.name);
        return request<SentFile>(files, { method: "POST", body }, session);
      }
      const prepared = await request<FileUploadStateResponse>(`${files}/uploads`, {
        method: "POST",
        body: JSON.stringify({
          token: input.token, propertyId: input.propertyId, requestId: input.requestId,
          name: input.file.name, size: input.file.size, contentType: input.file.type || "application/octet-stream",
        }),
      }, session);
      const upload = `${files}/uploads/${encodeURIComponent(prepared.uploadId)}`;
      for (let part = prepared.nextPart; part * prepared.partBytes < input.file.size; part += 1) {
        const piece = input.file.slice(part * prepared.partBytes, Math.min(input.file.size, (part + 1) * prepared.partBytes));
        await request(`${upload}/parts/${part}`, {
          method: "PUT",
          body: piece,
          headers: {
            "content-type": "application/octet-stream",
            "x-leafen-form-token": input.token,
            "x-leafen-part-sha256": await sha256Hex(await piece.arrayBuffer()),
          },
        }, session);
      }
      return request<SentFile>(`${upload}/complete`, {
        method: "POST", body: JSON.stringify({ token: input.token, requestId: input.requestId }),
      }, session);
    },
    // A place question's search by name, through the provider the server was given (form_place_search.go).
    searchFormPlaces: (
      input: { token: string; propertyId: string; query: string },
      session?: Session | null,
      signal?: AbortSignal,
    ) => request<PlaceSearchResponse>(`${formOperations.submit.path}/places/search`, {
      method: "POST", body: JSON.stringify(input), signal,
    }, session),
    copyFormResponse: (
      input: Omit<FormResponseCopyRequest, "action">,
      session?: Session | null,
    ) => request<FormResponseCopyResponse>(
      formOperations.responseCopy.path,
      {
        method: formOperations.responseCopy.method,
        body: JSON.stringify({ ...input, action: formActions.responseCopy }),
      },
      session,
    ),
  };
}

export type NativeFormApi = ReturnType<typeof createFormApi>;
