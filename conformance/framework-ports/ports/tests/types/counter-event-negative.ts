import { updateCounter, initialCounter } from "../../.generated/counter.mjs";

updateCounter(initialCounter(), { _tag: "CounterEvent.ClickedIncrement" });
