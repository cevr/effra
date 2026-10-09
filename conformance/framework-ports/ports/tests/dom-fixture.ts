import { Window } from "happy-dom";

const bindings = ["window", "document", "navigator", "location", "Node", "Element", "HTMLElement", "SVGElement", "DocumentFragment", "ShadowRoot", "Text", "Comment", "Event", "MouseEvent", "CustomEvent", "FocusEvent", "KeyboardEvent", "MutationObserver", "getComputedStyle", "requestAnimationFrame", "cancelAnimationFrame"];

export function createDomFixture() {
  const window = new Window({ url: "http://localhost/" });
  const previous = new Map<string, PropertyDescriptor | undefined>();
  for (const name of [...bindings, "IS_REACT_ACT_ENVIRONMENT"]) {
    previous.set(name, Object.getOwnPropertyDescriptor(globalThis, name));
    const value = name === "IS_REACT_ACT_ENVIRONMENT" ? true : Reflect.get(window, name);
    if (value !== undefined) Object.defineProperty(globalThis, name, { configurable: true, writable: true, value });
  }
  const host = document.createElement("div");
  document.body.append(host);
  return {
    host,
    async close() {
      await window.happyDOM.close();
      for (const [name, descriptor] of previous) {
        if (descriptor) Object.defineProperty(globalThis, name, descriptor);
        else Reflect.deleteProperty(globalThis, name);
      }
    }
  };
}

export function countText(host: HTMLElement): string {
  return host.querySelector("[data-role='count']")?.textContent ?? "";
}

export function click(host: HTMLElement, action: "increment" | "decrement" | "reset"): void {
  const button = host.querySelector<HTMLButtonElement>(`[data-action='${action}']`);
  if (!button) throw new Error(`missing ${action} button`);
  button.dispatchEvent(new MouseEvent("click", { bubbles: true }));
}

// Wait for the real DOM outcome. The timer is only a failure watchdog, never
// the synchronization or lifecycle proof.
export function waitForCountAndTitle(host: HTMLElement, count: string, title: string): Promise<void> {
  if (countText(host) === count && document.title === title) return Promise.resolve();
  return new Promise((resolve, reject) => {
    const observer = new MutationObserver(() => {
      if (countText(host) === count && document.title === title) {
        observer.disconnect(); clearTimeout(watchdog); resolve();
      }
    });
    const watchdog = setTimeout(() => {
      observer.disconnect(); reject(new Error(`DOM outcome missing: wanted ${count}/${title}, got ${countText(host)}/${document.title}`));
    }, 2000);
    observer.observe(document.documentElement, { subtree: true, childList: true, characterData: true, attributes: true });
  });
}
