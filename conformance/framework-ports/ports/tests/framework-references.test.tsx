import { test, expect } from "bun:test";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { RegistryContext } from "@effect/atom-react";
import { render as solidRender } from "@solidjs/web";
import { render as yieldRender } from "solid-yield";
import { createReferenceRegistry, type ReferenceRegistry } from "../src/reference-registry.js";
import { ReactReferenceCounter } from "../src/react-reference-counter.js";
import { SolidReferenceCounter } from "../src/solid-reference-counter.js";
import { makeYieldReferenceCounter } from "../src/yield-reference-counter.js";
import type { HostLifecycle } from "../src/reference-counter.js";
import { createDomFixture, countText, click, waitForCountAndTitle } from "./dom-fixture.js";

function listenerCount(owner: ReferenceRegistry): number {
  let count = 0;
  for (const node of owner.registry.getNodes().values()) count += node.listeners.size;
  return count;
}

for (const framework of ["react-atom", "solid-effect", "solid-yield"] as const) {
  test(`${framework} reference mounts, applies source events below zero, resets from nonzero and releases its subscriptions`, async () => {
    const dom = createDomFixture();
    const owner = createReferenceRegistry();
    const lifecycle: HostLifecycle[] = [];
    const onLifecycle = (event: HostLifecycle) => lifecycle.push(event);
    let unmount: (() => Promise<void>) | undefined;
    try {
      expect(listenerCount(owner)).toBe(0);
      if (framework === "react-atom") {
        const root = createRoot(dom.host);
        unmount = () => act(async () => { root.unmount(); });
        await act(async () => { root.render(<RegistryContext.Provider value={owner.registry}><ReactReferenceCounter owner={owner} onLifecycle={onLifecycle} /></RegistryContext.Provider>); });
      } else if (framework === "solid-effect") {
        const dispose = solidRender(() => SolidReferenceCounter(owner, onLifecycle), dom.host);
        unmount = async () => { dispose(); };
      } else {
        const dispose = yieldRender(makeYieldReferenceCounter(owner, onLifecycle), dom.host);
        unmount = async () => { dispose(); };
      }
      await waitForCountAndTitle(dom.host, "0", "Counter: 0");
      expect(dom.host.querySelector(`[data-reference='${framework}']`)).not.toBeNull();
      expect(lifecycle).toEqual(["mount"]);
      expect(listenerCount(owner)).toBeGreaterThan(0);
      const send = async (event: "increment" | "decrement" | "reset", expectedCount: string, expectedTitle: string) => {
        if (framework === "react-atom") await act(async () => { click(dom.host, event); });
        else click(dom.host, event);
        await waitForCountAndTitle(dom.host, expectedCount, expectedTitle);
        expect(countText(dom.host)).toBe(expectedCount);
        expect(document.title).toBe(expectedTitle);
      };
      await send("increment", "1", "Counter: 1");
      await send("decrement", "0", "Counter: 0");
      await send("decrement", "-1", "Counter: -1");
      await send("reset", "0", "Counter: 0");
      await send("increment", "1", "Counter: 1");
      await send("reset", "0", "Counter: 0");
      await unmount(); unmount = undefined;
      expect(lifecycle).toEqual(["mount", "unmount"]);
      expect(listenerCount(owner)).toBe(0);
      expect(dom.host.childNodes.length).toBe(0);
      owner.registry.dispose();
      expect(owner.registry.getNodes().size).toBe(0);
    } finally {
      if (unmount) await unmount();
      owner.registry.dispose();
      await dom.close();
    }
  });
}
