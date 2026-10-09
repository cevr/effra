// Number-domain control for the immutable Foldkit counter, not an Effra port.
export type ReferenceState = Readonly<{ count: number }>;
export type ReferenceEvent = "ClickedDecrement" | "ClickedIncrement" | "ClickedReset";
export type HostLifecycle = "mount" | "unmount";

export function initialReferenceState(): ReferenceState {
  return { count: 0 };
}

export function reduceReferenceCounter(state: ReferenceState, event: ReferenceEvent): ReferenceState {
  switch (event) {
    case "ClickedDecrement": return { count: state.count - 1 };
    case "ClickedIncrement": return { count: state.count + 1 };
    case "ClickedReset": return { count: 0 };
  }
}

export function referenceText(state: ReferenceState): string {
  return state.count.toString();
}

export function referenceTitle(state: ReferenceState): string {
  return `Counter: ${state.count}`;
}
