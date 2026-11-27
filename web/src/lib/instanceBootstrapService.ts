import { api } from "@/api";
import type {
  AdministratorSession,
  AdministratorSetupInput,
  AdministratorSetupStatus,
} from "@/administratorWire.generated";

export type InstanceBootstrapStatus = {
  masterConfigured: boolean;
  masterReady: boolean;
  setupBlocked: boolean;
  setupAvailable: boolean;
  setupAuthorizationRequired: boolean;
  setupInProgress: boolean;
};

export type InstanceBootstrapTransport = {
  setupStatus(): Promise<AdministratorSetupStatus>;
  claimAdministrator(input: AdministratorSetupInput): Promise<AdministratorSession>;
};

function retainedBootstrapStatus(configured: boolean, codeRequired: boolean): InstanceBootstrapStatus {
  return {
    masterConfigured: configured,
    masterReady: configured,
    setupBlocked: false,
    setupAvailable: !configured,
    // The first setup takes the one-time code from the server's setup link.
    setupAuthorizationRequired: !configured && codeRequired,
    setupInProgress: false,
  };
}

export function createInstanceBootstrapService(transport: InstanceBootstrapTransport) {
  // The setup state last read, which the setup screen's own lines draw from
  // with the screen it opened, not from a read of their own after it.
  let seen: AdministratorSetupStatus | null = null;
  const watchers = new Set<() => void>();
  return {
    async readStatus(): Promise<InstanceBootstrapStatus | null> {
      const status = await transport.setupStatus();
      seen = status;
      for (const watcher of watchers) watcher();
      return retainedBootstrapStatus(status.configured, Boolean(status.codeRequired));
    },
    /** The setup state the last readStatus read, or null before one. */
    seenStatus: (): AdministratorSetupStatus | null => seen,
    followStatus(watcher: () => void): () => void {
      watchers.add(watcher);
      return () => { watchers.delete(watcher); };
    },
    initialize(input: AdministratorSetupInput): Promise<AdministratorSession> {
      return transport.claimAdministrator(input);
    },
  };
}

export const instanceBootstrapService = createInstanceBootstrapService({
  setupStatus: () => api.setupStatus(),
  claimAdministrator: (input) => api.claimAdministrator(input),
});
