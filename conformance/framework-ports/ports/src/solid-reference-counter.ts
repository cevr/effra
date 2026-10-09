import { createEffect, createSignal, onCleanup } from "solid-js";
import h from "@solidjs/h";
import { referenceText, referenceTitle, type HostLifecycle } from "./reference-counter.js";
import type { ReferenceRegistry } from "./reference-registry.js";

export function SolidReferenceCounter(owner: ReferenceRegistry, onLifecycle: (event: HostLifecycle) => void) {
  const [state, setState] = createSignal(owner.registry.get(owner.state));
  const cancel = owner.registry.subscribe(owner.state, value => setState(value));
  onLifecycle("mount");
  onCleanup(() => { cancel(); onLifecycle("unmount"); });
  createEffect(() => referenceTitle(state()), title => { document.title = title; });
  const element = h(
    "section", { "data-reference": "solid-effect" },
    h("output", { "data-role": "count", "aria-live": "polite" }, () => referenceText(state())),
    h("button", { type: "button", "data-action": "decrement", "aria-label": "Decrement", onClick: () => owner.dispatch("ClickedDecrement") }, "-"),
    h("button", { type: "button", "data-action": "reset", onClick: () => owner.dispatch("ClickedReset") }, "Reset"),
    h("button", { type: "button", "data-action": "increment", "aria-label": "Increment", onClick: () => owner.dispatch("ClickedIncrement") }, "+")
  );
  return typeof element === "function" ? element() : element;
}
