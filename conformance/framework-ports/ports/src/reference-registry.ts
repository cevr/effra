import { Effect } from "effect";
import * as Atom from "effect/reactivity/Atom";
import * as AtomRegistry from "effect/reactivity/AtomRegistry";
import { scheduleTask } from "@effect/atom-react";
import { initialReferenceState, reduceReferenceCounter, type ReferenceEvent } from "./reference-counter.js";

// The mounting fixture owns this registry. Hosts release their subscriptions
// before the fixture disposes it; the stock RegistryProvider is not used here.
export function createReferenceRegistry() {
  const state = Atom.make(initialReferenceState());
  const registry = AtomRegistry.make({ scheduleTask });
  return {
    state,
    registry,
    dispatch(event: ReferenceEvent): void {
      Effect.runSync(Effect.sync(() => registry.update(state, current => reduceReferenceCounter(current, event))));
    }
  };
}

export type ReferenceRegistry = ReturnType<typeof createReferenceRegistry>;
