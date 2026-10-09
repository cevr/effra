import { createSignal } from "solid-js";
import { $cleanup, $effect, $event, $memo, component, view } from "solid-yield";
import { h, type HAttributes } from "solid-yield/h";
import { referenceText, referenceTitle, type HostLifecycle } from "./reference-counter.js";
import type { ReferenceRegistry } from "./reference-registry.js";

export function makeYieldReferenceCounter(owner: ReferenceRegistry, onLifecycle: (event: HostLifecycle) => void) {
  return component(function* YieldReferenceCounter() {
    // A public Solid accessor adapts the external registry subscription. A yield
    // setter cannot be handed to a plain subscription callback (upstream D-028).
    const [readState, setState] = createSignal(owner.registry.get(owner.state));
    const cancel = owner.registry.subscribe(owner.state, value => setState(value));
    const state = yield* $memo(function* () { return readState(); });
    onLifecycle("mount");
    yield* $cleanup(() => { cancel(); onLifecycle("unmount"); });
    yield* $effect(
      function* () { return referenceTitle(yield* state); },
      function* (title) { document.title = title; }
    );
    const decrement = $event(function* () { owner.dispatch("ClickedDecrement"); });
    const reset = $event(function* () { owner.dispatch("ClickedReset"); });
    const increment = $event(function* () { owner.dispatch("ClickedIncrement"); });
    return view(function* () {
      const section = { ref: node => node.setAttribute("data-reference", "solid-yield") } satisfies HAttributes<"section">;
      const output = { ref: node => node.setAttribute("data-role", "count"), "aria-live": "polite" } satisfies HAttributes<"output">;
      const decrementButton = { type: "button", "aria-label": "Decrement", ref: node => node.setAttribute("data-action", "decrement"), onClick: decrement } satisfies HAttributes<"button">;
      const resetButton = { type: "button", ref: node => node.setAttribute("data-action", "reset"), onClick: reset } satisfies HAttributes<"button">;
      const incrementButton = { type: "button", "aria-label": "Increment", ref: node => node.setAttribute("data-action", "increment"), onClick: increment } satisfies HAttributes<"button">;
      return h("section", section,
        h("output", output, function* () { return referenceText(yield* state); }),
        h("button", decrementButton, "-"), h("button", resetButton, "Reset"), h("button", incrementButton, "+"));
    });
  });
}
