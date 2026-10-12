// Host adapter cost of the laws prototype (#125, review 1 m5). Calls each
// export of proto/lib.mjs in a loop and prints raw per-call samples.
//   oldest     no adapter: the control, an ordinary export over the carrier
//   sortTrio   outward adapter: snapshot of the argument, deep freeze of the result
//   search     inward adapter: snapshot of both arguments and the sortedTrio
//              admission check before the call
// Usage: node benchmarks/laws/ts/adapter-cost.mjs [iterations] [samples]
import { makeUser, makeTrio, sortTrio, search, oldest } from './proto/lib.mjs';

const iterations = Number(process.argv[2] ?? 200000);
const samples = Number(process.argv[3] ?? 7);
const ada = makeUser("ada", 36n);
const bob = makeUser("bob", 12n);
const cy = makeUser("cy", 24n);
const trio = makeTrio(ada, bob, cy);
const sorted = sortTrio(trio);

const cases = {
  oldest: () => oldest(trio),
  sortTrio: () => sortTrio(trio),
  search: () => search(sorted, cy),
};

let sink = 0;
const run = (f) => {
  const start = process.hrtime.bigint();
  for (let i = 0; i < iterations; i++) {
    if (f()) sink++;
  }
  return Number(process.hrtime.bigint() - start) / iterations;
};

for (const [name, f] of Object.entries(cases)) {
  run(f); // warm-up sample, discarded
  const raw = [];
  for (let s = 0; s < samples; s++) raw.push(run(f));
  const sortedRaw = [...raw].sort((a, b) => a - b);
  const median = sortedRaw[Math.floor(sortedRaw.length / 2)];
  console.log(`${name} ns/call median=${median.toFixed(1)} raw=${raw.map((x) => x.toFixed(1)).join(",")}`);
}
if (sink === 0) throw new Error("adapter-cost: the loop did nothing");
