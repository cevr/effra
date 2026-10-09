import { afterAll } from "bun:test";
import { createDomFixture } from "./dom-fixture.js";

if (process.env.NODE_ENV !== "test") throw new Error("mounted references require NODE_ENV=test for React act");
// Renderer imports cache DOM availability. Install this process owner first;
// each test restores it after closing its own fixture.
const processDom = createDomFixture();
afterAll(async () => { await processDom.close(); });
console.log("mounted process DOM bootstrap installed before renderer imports");
