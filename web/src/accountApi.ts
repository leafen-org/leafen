import type { Session } from "./types";
import type {
  AccountDeletionAccepted,
  AccountDeviceCode,
  AccountDeletionInput,
  AccountDeletionPreview,
  AccountEmailAddInput,
  AccountEmailConfirmInput,
  AccountEmailMutationInput,
  AccountEmailPending,
  AccountEmailState,
  AccountLanguagePreference,
  AccountLanguageState,
  AccountPasswordChangeInput,
  AccountPasswordState,
  AccountProfile,
  AccountProfileMutationInput,
  AccountProfileMutationResponse,
  AuthAccepted,
  PasswordResetConfirmResult,
  AuthAppSignIn,
  AuthAppSignInInput,
  AuthMFAChallenge,
  AuthMFAChallengeInput,
  AuthMFAConfirmInput,
  AuthMFAEnrollment,
  AuthMFARecoveryCodes,
  AuthMFARecoveryInput,
  AuthMFARevalidationInput,
  AuthMFAState,
  AuthProviderList,
  AuthSession,
  LoginInput,
  RegistrationVerification,
  SignupInvitation,
} from "./authWire.generated";
import { API_ROOT, request } from "./apiTransport";
import { serverPath } from "./lib/serverOrigin";

export function createAccountApi() {
  return {
    readAccountEmails: (session: Session, signal?: AbortSignal) => request<AccountEmailState>("/account/emails", { signal }, session),
    // A server that sends no mail adds it at once and answers with the addresses.
    addAccountEmail: (session: Session, input: AccountEmailAddInput, signal?: AbortSignal) => request<AccountEmailPending | AccountEmailState>(
      "/account/emails", { method: "POST", body: JSON.stringify(input), signal }, session,
    ),
    confirmAccountEmail: (session: Session, input: AccountEmailConfirmInput, signal?: AbortSignal) => request<AccountEmailState>(
      "/account/emails/confirm", { method: "POST", body: JSON.stringify(input), signal }, session,
    ),
    promoteAccountEmail: (session: Session, input: AccountEmailMutationInput, signal?: AbortSignal) => request<AccountEmailState>(
      "/account/emails/primary", { method: "PUT", body: JSON.stringify(input), signal }, session,
    ),
    removeAccountEmail: (session: Session, input: AccountEmailMutationInput, signal?: AbortSignal) => request<AccountEmailState>(
      "/account/emails", { method: "DELETE", body: JSON.stringify(input), signal }, session,
    ),
    signupInvitation: (token: string) => request<SignupInvitation>(
      "/auth/signup-invitation", { method: "POST", body: JSON.stringify({ token }) },
    ),
    // An email invitation the signed-in account holds the address of (components/auth/takeSignupInvitation.ts).
    takeSignupInvitation: (session: Session, token: string) => request<{ workspaceId: string; pageId?: string }>(
      "/auth/invitation/take", { method: "POST", body: JSON.stringify({ token }) }, session,
    ),
    createAccount: (email: string, password: string, name: string, invitation = "") =>
      request<Session | RegistrationVerification>(
        "/auth/register",
        { method: "POST", body: JSON.stringify({ email, password, name, ...(invitation ? { invitation } : {}) }) },
      ),
    login: (
      email: string,
      password: string,
      options: Pick<LoginInput, "requestId" | "replaceOldestSession">,
    ) => request<Session | AuthMFAChallenge>("/auth/login", {
      method: "POST",
      body: JSON.stringify({ email, password, ...options }),
    }),
    authProviders: () => request<AuthProviderList>("/auth/providers"),
    authProviderStartURL: (provider: string) => serverPath(
      `${API_ROOT}/auth/providers/${encodeURIComponent(provider)}/start`,
    ),
    authSAMLStartURL: (providerOrOrganization: string, redirect: string) => serverPath(
      `${API_ROOT}/auth/saml/${encodeURIComponent(providerOrOrganization)}/start`
      + `?redirect=${encodeURIComponent(redirect)}`,
    ),
    // A code an app's sign-in brought back goes with its PKCE verifier.
    // An account with 2-step verification is asked for its code next, as after its password.
    exchangeAuthProvider: (code: string, verifier?: string) => request<Session | AuthMFAChallenge>(
      "/auth/providers/exchange", { method: "POST", body: JSON.stringify(verifier ? { code, verifier } : { code }) },
    ),
    // A work email's company sign-in, by its domain (organization/enterprise_saml_find.go).
    findCompanySignIn: (email: string) => request<{ organizationId: string }>(
      "/auth/saml/find", { method: "POST", body: JSON.stringify({ email }) },
    ),
    exchangeSAMLCompletion: (code: string, verifier?: string) => request<Session>(
      "/auth/saml/exchange", { method: "POST", body: JSON.stringify(verifier ? { code, verifier } : { code }) },
    ),
    // The sign-in page asks the server to show its administrator a one-time
    // code in its own log while none has it open; it answers whether one is
    // shown there, never the code (administrator_sign_in_code.go).
    askAdministratorSignInCode: () => request<{ shown: boolean }>("/auth/administrator-code", { method: "POST" }),
    exchangeLocalOwnerCapability: (code: string) => request<Session>(
      "/local-owner/session", { method: "POST", body: JSON.stringify({ code }) },
    ),
    // An invitation opened: a name, and this device is in (auth_invitation_join.go).
    joinWithInvitation: (invitation: string, name: string) => request<Session>(
      "/auth/invitation/join", { method: "POST", body: JSON.stringify({ invitation, name }) },
    ),
    settleLocalOwner: (session: Session, code: string) => request<void>(
      "/local-owner/settled", { method: "POST", body: JSON.stringify({ code }) }, session,
    ),
    requestEmailVerification: (email: string) => request<AuthAccepted>(
      "/auth/verification/request", { method: "POST", body: JSON.stringify({ email }) },
    ),
    confirmEmailVerification: (token: string) => request<Session>(
      "/auth/verification/confirm", { method: "POST", body: JSON.stringify({ token }) },
    ),
    requestPasswordReset: (email: string) => request<AuthAccepted>(
      "/auth/password-reset/request", { method: "POST", body: JSON.stringify({ email }) },
    ),
    /** Whether a reset link can still be used, without using it. */
    checkPasswordReset: (token: string) => request<void>(
      "/auth/password-reset/check", { method: "POST", body: JSON.stringify({ token }) },
    ),
    confirmPasswordReset: (token: string, password: string, passwordConfirm: string) => request<PasswordResetConfirmResult>(
      "/auth/password-reset/confirm",
      { method: "POST", body: JSON.stringify({ token, password, passwordConfirm }) },
    ),
    changeAccountPassword: (session: Session, input: AccountPasswordChangeInput) => request<Session>(
      "/account/password", { method: "PUT", body: JSON.stringify(input) }, session,
    ),
    // A password for an account whose person knows none, and forgetting it (account_password.go).
    setAccountPassword: (session: Session, input: AccountPasswordChangeInput) => request<void>(
      "/account/password", { method: "POST", body: JSON.stringify(input) }, session,
    ),
    forgetAccountPassword: (session: Session) => request<void>(
      "/account/password", { method: "DELETE" }, session,
    ),
    readAccountState: (session: Session, signal?: AbortSignal) => request<AccountPasswordState>(
      "/account/state", { signal }, session,
    ),
    readAccountDeletion: (session: Session, signal?: AbortSignal) => request<AccountDeletionPreview>(
      "/account/deletion", { signal }, session,
    ),
    deleteAccount: (session: Session, input: AccountDeletionInput) => request<AccountDeletionAccepted>(
      "/account/deletion", { method: "POST", body: JSON.stringify(input) }, session,
    ),
    listAuthSessions: (session: Session, signal?: AbortSignal) => request<AuthSession[]>(
      "/account/sessions", { signal }, session,
    ),
    revokeAuthSession: (session: Session, sessionId: string) => request<void>(
      `/account/sessions/${encodeURIComponent(sessionId)}`,
      { method: "DELETE" },
      session,
    ),
    // The asking sign-in confirmed with a 2-step code, and the account's other sign-ins
    // without a second factor signed out, as a 2-step policy may ask.
    confirmSessionSecondFactor: (session: Session, code: string) => request<void>(
      "/account/sessions/current/second-factor", { method: "POST", body: JSON.stringify({ code }) }, session,
    ),
    revokeSessionsWithoutSecondFactor: (session: Session) => request<void>(
      "/account/sessions/without-second-factor/revoke", { method: "POST" }, session,
    ),
    // A one-time code (and its QR address) for another device to join this account.
    createDeviceCode: (session: Session) => request<AccountDeviceCode>(
      "/account/device-codes", { method: "POST" }, session,
    ),
    cancelDeviceCode: (session: Session, codeId: string) => request<void>(
      `/account/device-codes/${encodeURIComponent(codeId)}`, { method: "DELETE" }, session,
    ),
    // At the account's sign-in limit, replaceOldestSession ends its oldest sign-in for this one.
    joinWithDeviceCode: (code: string, replaceOldestSession = false) => request<Session>(
      "/auth/device", { method: "POST", body: JSON.stringify(replaceOldestSession ? { code, replaceOldestSession } : { code }) },
    ),
    // An app signing in through this browser: its code goes to the app's return.
    registerAppByBrowser: (session: Session, input: AuthAppSignInInput) => request<AuthAppSignIn>(
      "/account/app-sign-in", { method: "POST", body: JSON.stringify(input) }, session,
    ),
    exchangeAppSignIn: (code: string, verifier: string) => request<Session>(
      "/auth/app-sign-in/exchange", { method: "POST", body: JSON.stringify({ code, verifier }) },
    ),
    readAccountMFA: (session: Session, signal?: AbortSignal) => request<AuthMFAState>(
      "/account/mfa", { signal }, session,
    ),
    enrollAccountTOTP: (session: Session) => request<AuthMFAEnrollment>(
      "/account/mfa/totp/enroll", { method: "POST" }, session,
    ),
    confirmAccountTOTP: (session: Session, input: AuthMFAConfirmInput) => request<AuthMFAState>(
      "/account/mfa/totp/confirm", { method: "POST", body: JSON.stringify(input) }, session,
    ),
    disableAccountTOTP: (session: Session, input: AuthMFARevalidationInput) => request<void>(
      "/account/mfa/totp", { method: "DELETE", body: JSON.stringify(input) }, session,
    ),
    regenerateAccountMFARecoveryCodes: (
      session: Session,
      input: AuthMFARevalidationInput,
    ) => request<AuthMFARecoveryCodes>(
      "/account/mfa/recovery-codes", { method: "POST", body: JSON.stringify(input) }, session,
    ),
    completeTOTPChallenge: (input: AuthMFAChallengeInput) => request<Session>(
      "/auth/mfa/totp", { method: "POST", body: JSON.stringify(input) },
    ),
    completeRecoveryChallenge: (input: AuthMFARecoveryInput) => request<Session>(
      "/auth/mfa/recovery", { method: "POST", body: JSON.stringify(input) },
    ),
    /** The order the person dragged their workspaces to in the workspace menu, on every device (account_workspace_order.go). */
    readWorkspaceOrder: (session: Session, signal?: AbortSignal) => request<{ workspaceIds: string[] }>(
      "/account/workspace-order", { signal }, session,
    ),
    saveWorkspaceOrder: (session: Session, workspaceIds: readonly string[]) => request<{ workspaceIds: string[] }>(
      "/account/workspace-order", { method: "PUT", body: JSON.stringify({ workspaceIds }) }, session,
    ),
    readAccountLanguage: (session: Session, signal?: AbortSignal) => request<AccountLanguageState>(
      "/account/language", { signal }, session,
    ),
    saveAccountLanguage: (session: Session, languagePreference: AccountLanguagePreference) =>
      request<AccountLanguageState>(
        "/account/language", { method: "PUT", body: JSON.stringify({ languagePreference }) }, session,
      ),
    readAccountProfile: (session: Session, signal?: AbortSignal) => request<AccountProfile>(
      "/account/profile", { signal }, session,
    ),
    saveAccountProfile: (
      session: Session,
      input: AccountProfileMutationInput,
      signal?: AbortSignal,
    ) => request<AccountProfileMutationResponse>(
      "/account/profile", { method: "PATCH", body: JSON.stringify(input), signal }, session,
    ),
    /** Keeps a photo with the account and answers its address, to save as the profile's picture. */
    uploadAccountPhoto: (session: Session, photo: Blob, signal?: AbortSignal) => request<{ avatar: string }>(
      "/account/avatar-photo", { method: "POST", body: photo, headers: { "content-type": photo.type }, signal }, session,
    ),
  };
}
