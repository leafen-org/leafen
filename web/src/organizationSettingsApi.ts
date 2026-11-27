import { request } from "@/apiTransport";
import {
  organizationOperations,
  type OrganizationSettingsMutationInput,
  type OrganizationSettingsMutationResponse,
  type OrganizationSettingsSnapshot,
} from "@/organizationWire.generated";
import type { Session } from "@/types";

export function readOrganizationSettingsSnapshot(
  session: Session,
  organizationId: string,
  cursor: string,
  signal: AbortSignal,
) {
  const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
  return request<OrganizationSettingsSnapshot>(
    `${organizationOperations.settingsSnapshot.path.replace("{organizationId}", encodeURIComponent(organizationId))}${query}`,
    { method: organizationOperations.settingsSnapshot.method, signal },
    session,
  );
}

export function writeOrganizationSettingsMutation(
  session: Session,
  organizationId: string,
  input: OrganizationSettingsMutationInput,
  signal: AbortSignal,
) {
  return request<OrganizationSettingsMutationResponse>(
    organizationOperations.mutateSettings.path.replace("{organizationId}", encodeURIComponent(organizationId)),
    {
      body: JSON.stringify(input),
      method: organizationOperations.mutateSettings.method,
      signal,
    },
    session,
  );
}
