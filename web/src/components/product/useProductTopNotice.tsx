import type { ReactNode } from "react";
import { useBackupNotice } from "@/components/admin/BackupFailureNotice";
import { useSecurityUpdateNotice } from "@/components/admin/SecurityUpdateNotice";
import { UnenforcedPoliciesNotice } from "@/components/admin/UnenforcedPoliciesNotice";
import type { AdministratorSettings } from "@/administratorWire.generated";
import type { Session } from "@/types";
import { useAddressHandoffNotice } from "./AddressHandoffNotice";
import { useAppUpdateNotice } from "./AppUpdateNotice";
import { useProperAddressOffer } from "./ProperAddressOffer";
import { useSteadyBand } from "@/components/shell/useSteadyBand";
import { useAppFullWindow } from "@/lib/appFullWindow";

/**
 * The one band over the app. A server whose data moved elsewhere comes
 * first; then a sign-in just carried over from the server's earlier address,
 * which says so once and offers what that address held (2026-11-07: a
 * standing band, such as backups being off, kept it from showing at all);
 * organization policies the server doesn't enforce come before a security
 * release, and that before a failing backup. A restored version says so in
 * its page (PageRestoredNotice). An app's full window keeps only its own bar:
 * advice waits until it gives the window back, and only a band saying
 * changes aren't kept stays over it.
 */
export function useProductTopNotice({ session, administratorSettings, administratorKnown, openServerData, pathname, serverMovedNotice }: {
  session: Session;
  administratorSettings: AdministratorSettings | null;
  /** Whether the server has said if the account administers it. */
  administratorKnown: boolean;
  openServerData: (section?: "data" | "standby" | "updates") => void;
  pathname: string;
  serverMovedNotice: ReactNode;
}): ReactNode {
  const backupNotice = useBackupNotice(session, administratorSettings !== null, openServerData, administratorKnown);
  const securityNotice = useSecurityUpdateNotice(administratorSettings, () => openServerData("updates"));
  const appUpdateNotice = useAppUpdateNotice();
  const handoffNotice = useAddressHandoffNotice(session);
  const properAddressOffer = useProperAddressOffer(session);
  // What only advises comes in and goes with a page (useSteadyBand); what
  // says changes aren't kept comes at once.
  const advice = useSteadyBand(handoffNotice ?? (administratorSettings?.unenforcedOrganizationPolicies?.length
    ? <UnenforcedPoliciesNotice policies={administratorSettings.unenforcedOrganizationPolicies} />
    : securityNotice ?? backupNotice ?? properAddressOffer ?? appUpdateNotice), pathname);
  const fullWindow = useAppFullWindow();
  return serverMovedNotice ?? (fullWindow ? null : advice);
}
