// Executes the target-neutral JSON codec policy vectors against pinned Effect
// Schema 4.0.1 and prints one canonical JSON line per vector. The retained
// effect-comparator.out.jsonl is the exact comparator evidence; the Go vector
// test re-executes this script and requires byte equality before comparing
// it with the Effra engines.
//
// Plans become ordinary Schema declarations: String, Boolean, Void, Struct,
// Union of TaggedStruct, and BigInt with an explicit signed 64-bit
// isBetweenBigInt check (Effra's range is added comparator work, not
// upstream BigInt behavior). Shared plan nodes reuse one schema. The boundary
// is Schema.fromJsonString(Schema.toCodecJson(root)) with default parse
// options over WHATWG TextDecoder text, so JSON.parse, host text decoding and
// Schema's first issue are observed as executed rather than inferred.
import { readFileSync } from "node:fs";
import { Exit, Schema, SchemaIssue } from "effect";

const vectorsPath = process.argv[2] ?? new URL("./json-structural-vectors.json", import.meta.url);
const document = JSON.parse(readFileSync(vectorsPath, "utf8"));
const firstIssue = SchemaIssue.makeFormatterStandardSchemaV1();
const i64 = Schema.BigInt.check(Schema.isBetweenBigInt({ minimum: -(2n ** 63n), maximum: 2n ** 63n - 1n }));

const codecs = new Map();
const codecFor = name => {
  if (codecs.has(name)) return codecs.get(name);
  const plan = document.plans[name];
  const schemas = new Map();
  const fields = list => Object.fromEntries(list.map(field => [field.name, schemaFor(field.node)]));
  const schemaFor = index => {
    if (schemas.has(index)) return schemas.get(index);
    const node = plan.nodes[index];
    let schema;
    switch (node.kind) {
      case "string": schema = Schema.String; break;
      case "bool": schema = Schema.Boolean; break;
      case "void": schema = Schema.Void; break;
      case "i64": schema = i64; break;
      case "record": schema = Schema.Struct(fields(node.fields)); break;
      case "union": schema = Schema.Union(node.variants.map(variant => Schema.TaggedStruct(variant.tag, fields(variant.fields)))); break;
      default: throw new Error(`unsupported plan kind ${node.kind}`);
    }
    schemas.set(index, schema);
    return schema;
  };
  const codec = Schema.fromJsonString(Schema.toCodecJson(schemaFor(plan.root)));
  codecs.set(name, codec);
  return codec;
};

// Neutral encode values: {"$i64": "decimal"}, {"$void": true},
// {"$variant": "Tag", "fields": {...}}, {"$utf16": [units]}; records are objects.
const domainValue = (plan, index, value) => {
  const node = plan.nodes[index];
  const fields = (list, source) => list.map(field => [field.name, domainValue(plan, field.node, source[field.name])]);
  switch (node.kind) {
    case "string": return typeof value === "string" ? value : String.fromCharCode(...value.$utf16);
    case "bool": return value;
    case "void": return undefined;
    case "i64": return BigInt(value.$i64);
    case "record": return Object.fromEntries(fields(node.fields, value));
    case "union": {
      const variant = node.variants.find(candidate => candidate.tag === value.$variant);
      return Object.fromEntries([["_tag", variant.tag], ...fields(variant.fields, value.fields)]);
    }
  }
  throw new Error(`unsupported plan kind ${node.kind}`);
};

const settle = (id, exit) => {
  if (Exit.isSuccess(exit)) return { id, ok: true, encoded: exit.value };
  const reasons = exit.cause.reasons;
  if (reasons.length !== 1 || reasons[0]._tag !== "Fail" || reasons[0].error?.issue === undefined) throw new Error(`${id}: comparator failed outside Schema`);
  const [issue] = firstIssue(reasons[0].error.issue).issues;
  return { id, ok: false, path: issue.path.map(String), message: issue.message };
};

const lines = document.vectors.map(vector => {
  if (vector.comparison === "not-applicable") return { id: vector.id, skipped: "not-applicable" };
  const codec = codecFor(vector.plan);
  if (vector.direction === "decode") {
    const bytes = vector.bodyHex === undefined ? new TextEncoder().encode(vector.body) : Uint8Array.from(Buffer.from(vector.bodyHex, "hex"));
    const decoded = Schema.decodeUnknownExit(codec)(new TextDecoder().decode(bytes));
    return Exit.isSuccess(decoded) ? settle(vector.id, Schema.encodeUnknownExit(codec)(decoded.value)) : settle(vector.id, decoded);
  }
  return settle(vector.id, Schema.encodeUnknownExit(codec)(domainValue(document.plans[vector.plan], document.plans[vector.plan].root, vector.value)));
});
process.stdout.write(lines.map(line => JSON.stringify(line)).join("\n") + "\n");
