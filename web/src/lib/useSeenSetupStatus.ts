import { useSyncExternalStore } from "react";
import type { AdministratorSetupStatus } from "@/administratorWire.generated";
import { instanceBootstrapService } from "./instanceBootstrapService";

/**
 * The setup state the sign-in page's start read before it opened the setup
 * screen (authGateStart.ts): the screen's lines that depend on it draw with
 * the screen.
 */
export function useSeenSetupStatus(): AdministratorSetupStatus | null {
  return useSyncExternalStore(instanceBootstrapService.followStatus, instanceBootstrapService.seenStatus);
}
