export const authCompletionBounds = Object.freeze({ inFlight: 8 });

const completions = new Map<string, Promise<unknown>>();

export function completeAuthenticationOnce<T>(
  key: string,
  complete: () => Promise<T>,
): Promise<T> {
  const normalizedKey = key.trim();
  const existing = completions.get(normalizedKey);
  if (existing) return existing as Promise<T>;
  if (!normalizedKey || completions.size >= authCompletionBounds.inFlight) {
    return Promise.reject(new RangeError("Authentication callback capacity reached."));
  }

  let request: Promise<T>;
  try {
    request = Promise.resolve(complete());
  } catch (error) {
    request = Promise.reject(error);
  }
  completions.set(normalizedKey, request);
  const clear = () => {
    if (completions.get(normalizedKey) === request) completions.delete(normalizedKey);
  };
  void request.then(clear, clear);
  return request;
}
