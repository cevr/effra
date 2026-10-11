// Executes the target-neutral JSON codec transformation vectors against
// pinned Effect Schema 4.0.1 and prints one canonical JSON line per vector.
// The retained effect-transform-comparator.out.jsonl is the exact comparator
// evidence; the Go transformation vector test re-executes this script and
// requires byte equality before comparing it with the Effra engines.
//
// A transform node becomes `child.pipe(Schema.decodeTo(to, { decode, encode }))`
// with SchemaGetter.transformEffect for a behavior that can fail (failing
// with SchemaIssue.InvalidValue carrying the behavior's message) and
// SchemaGetter.transform for one that cannot. Records and unions are the
// same ordinary declarations the structural comparator uses. Schema runs a
// transformation as soon as its own subtree decodes, so its first issue is
// observed per field, not after the whole structure.
import { readFileSync } from "node:fs";
import { Effect, Exit, Schema, SchemaGetter, SchemaIssue } from "effect";

const vectorsPath = process.argv[2] ?? new URL("./json-transform-vectors.json", import.meta.url);
const document = JSON.parse(readFileSync(vectorsPath, "utf8"));
const firstIssue = SchemaIssue.makeFormatterStandardSchemaV1();

const refuse = message => Effect.fail(new SchemaIssue.InvalidValue({ message }));
const behaviors = {
  email: child => child.pipe(Schema.decodeTo(Schema.String, {
    decode: SchemaGetter.transformEffect(text => text === "" ? refuse("empty") : Effect.succeed(text.toLowerCase())),
    encode: SchemaGetter.transformEffect(text => text === "blocked" ? refuse("blocked") : Effect.succeed(text)),
  })),
  status: child => child.pipe(Schema.decodeTo(Schema.Literals(["Active", "Suspended"]), {
    decode: SchemaGetter.transform(wire => wire._tag === "Suspended" ? "Suspended" : "Active"),
    encode: SchemaGetter.transform(status => ({ _tag: status })),
  })),
  box: child => child.pipe(Schema.decodeTo(Schema.String, {
    decode: SchemaGetter.transform(box => box.value),
    encode: SchemaGetter.transform(value => ({ value })),
  })),
};

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
      case "record": schema = Schema.Struct(fields(node.fields)); break;
      case "union": schema = Schema.Union(node.variants.map(variant => Schema.TaggedStruct(variant.tag, fields(variant.fields)))); break;
      case "transform": schema = behaviors[node.behavior](schemaFor(node.child)); break;
      default: throw new Error(`unsupported plan kind ${node.kind}`);
    }
    schemas.set(index, schema);
    return schema;
  };
  const codec = Schema.fromJsonString(Schema.toCodecJson(schemaFor(plan.root)));
  codecs.set(name, codec);
  return codec;
};

// Neutral encode values are domain values: records are objects and a
// transformed position holds its behavior's domain value.
const settle = (id, exit) => {
  if (Exit.isSuccess(exit)) return { id, ok: true, encoded: exit.value };
  const reasons = exit.cause.reasons;
  if (reasons.length !== 1 || reasons[0]._tag !== "Fail" || reasons[0].error?.issue === undefined) throw new Error(`${id}: comparator failed outside Schema`);
  const [issue] = firstIssue(reasons[0].error.issue).issues;
  return { id, ok: false, path: issue.path.map(String), message: issue.message };
};

const lines = document.vectors.map(vector => {
  const codec = codecFor(vector.plan);
  if (vector.direction === "decode") {
    const decoded = Schema.decodeUnknownExit(codec)(vector.body);
    return Exit.isSuccess(decoded) ? settle(vector.id, Schema.encodeUnknownExit(codec)(decoded.value)) : settle(vector.id, decoded);
  }
  return settle(vector.id, Schema.encodeUnknownExit(codec)(vector.value));
});
process.stdout.write(lines.map(line => JSON.stringify(line)).join("\n") + "\n");
