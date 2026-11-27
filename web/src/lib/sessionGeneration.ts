type SessionGenerationResetListener = () => void;

const resetListeners = new Set<SessionGenerationResetListener>();

export function subscribeSessionGenerationReset(
  listener: SessionGenerationResetListener,
): () => void {
  resetListeners.add(listener);
  return () => resetListeners.delete(listener);
}

export function publishSessionGenerationReset(): void {
  for (const listener of [...resetListeners]) listener();
}
