export interface AuthOperationLane {
  begin(): number;
  current(): number;
  isCurrent(generation: number): boolean;
  retire(): void;
}

export function createAuthOperationLane(): AuthOperationLane {
  let generation = 0;
  return Object.freeze({
    begin() {
      generation += 1;
      return generation;
    },
    current() {
      return generation;
    },
    isCurrent(candidate: number) {
      return candidate === generation;
    },
    retire() {
      generation += 1;
    },
  });
}
