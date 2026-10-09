import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { lstat, readFile, readdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const provenance = JSON.parse(await readFile(path.join(root, "vendor/solid-yield.provenance.json"), "utf8"));
assert.equal(provenance.commit, "2f2431da101ffc1c0e3fde7b5aa4fcb6b009584c");
assert.equal(provenance.files.length, 25);
const expected = new Set(provenance.files.map(file => file.path));
for (const file of provenance.files) {
  const filename = path.join(root, file.path);
  const info = await lstat(filename);
  assert.ok(info.isFile(), `capture must be a regular file: ${file.path}`);
  assert.equal(info.mode & 0o777, file.gitMode === "100755" ? 0o755 : 0o644, file.path);
  const bytes = await readFile(filename);
  assert.equal(bytes.length, file.bytes, file.path);
  assert.equal(createHash("sha256").update(bytes).digest("hex"), file.sha256, file.path);
  assert.equal(createHash("sha1").update(`blob ${bytes.length}\0`).update(bytes).digest("hex"), file.gitBlob, file.path);
}
async function checkDirectory(directory) {
  for (const entry of await readdir(path.join(root, directory), { withFileTypes: true })) {
    const relative = `${directory}/${entry.name}`;
    if (relative === "vendor/solid-yield/dist" || relative === "vendor/solid-yield/node_modules") continue;
    if (entry.isDirectory()) await checkDirectory(relative);
    else assert.ok(expected.has(relative), `unexpected captured path: ${relative}`);
  }
}
await checkDirectory("vendor/solid-yield");
console.log(`yield capture: ${expected.size} exact licensed source paths at ${provenance.commit}`);
