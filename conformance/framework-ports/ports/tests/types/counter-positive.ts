import { initialCounter, counterState, clickedDecrement, clickedIncrement, clickedReset, updateCounter, counterCount } from "../../.generated/counter.mjs";
import type { CounterState, CounterEvent } from "../../.generated/counter.mjs";

const state: CounterState = counterState(0n);
const event: CounterEvent = clickedIncrement();
const count: bigint = counterCount(updateCounter(state, event));
const decremented: CounterState = updateCounter(initialCounter(), clickedDecrement());
const reset: CounterState = updateCounter(decremented, clickedReset());
void count; void reset;
