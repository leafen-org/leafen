import { isSignedOutResponse, signInHandingOver } from "./signedOutResponse";

// Whether a read that failed was the server's own answer that the reader may
// not have it — signed out, refused or missing — and not a read that just
// didn't get through: what was shown or kept of it goes then.
//
// A signed-out answer while this browser hands its sign-in over (a password
// change, lib/signedOutResponse.ts) is not one: the sign-in it was asked with
// is being replaced, and what it may read is known once the next one is here.
// What shows stays and the kept copy stays, and the read is made again with
// the next sign-in (2026-11-14: a tab made visible meanwhile read its list's
// refusal as final, showed no workspace and dropped its kept copy for the
// seconds the change's answer took). A sign-in that really ended — signed
// out elsewhere — has no handover here, and is refused at once as before.

function errorField(error: unknown, field: string): unknown {
  return error && typeof error === "object" ? Reflect.get(error, field) : undefined;
}

/** Whether `error` is a signed-out answer given while this browser hands its sign-in over. */
export function refusedDuringSignInHandover(error: unknown): boolean {
  return isSignedOutResponse(errorField(error, "status"), errorField(error, "code")) && signInHandingOver();
}

/** Whether a read's failure is the server's final answer that the reader may not have it. */
export function isDefinitiveReadDenial(error: unknown): boolean {
  const status = errorField(error, "status");
  if (status === 401) return !refusedDuringSignInHandover(error);
  return status === 403 || status === 404;
}
