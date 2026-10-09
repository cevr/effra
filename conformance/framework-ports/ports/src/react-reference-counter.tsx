import { useEffect } from "react";
import { useAtomValue } from "@effect/atom-react";
import { referenceText, referenceTitle, type HostLifecycle } from "./reference-counter.js";
import type { ReferenceRegistry } from "./reference-registry.js";

type Props = Readonly<{ owner: ReferenceRegistry; onLifecycle: (event: HostLifecycle) => void }>;

export function ReactReferenceCounter({ owner, onLifecycle }: Props) {
  const state = useAtomValue(owner.state);
  useEffect(() => {
    onLifecycle("mount");
    return () => onLifecycle("unmount");
  }, [onLifecycle]);
  useEffect(() => { document.title = referenceTitle(state); }, [state]);
  return (
    <section data-reference="react-atom">
      <output data-role="count" aria-live="polite">{referenceText(state)}</output>
      <button type="button" data-action="decrement" aria-label="Decrement" onClick={() => owner.dispatch("ClickedDecrement")}>-</button>
      <button type="button" data-action="reset" onClick={() => owner.dispatch("ClickedReset")}>Reset</button>
      <button type="button" data-action="increment" aria-label="Increment" onClick={() => owner.dispatch("ClickedIncrement")}>+</button>
    </section>
  );
}
