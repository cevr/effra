// Bounded JSON codec engine for the profile effra/json-structural-1. Generated
// modules supply plan literals and ordinary Effra JS carriers: records are
// plain objects, union variants carry their domain `_tag`, i64 is bigint and
// void is undefined. The engine owns admission, structural decoding and
// encoding, mirroring runtime/effra/codec.go: the same plan rules, failure
// reasons, declared-key paths and admission byte offsets. Expected failures
// are returned as data; plan and carrier mismatches are defects (thrown).
const __ef_codecProfile = "effra/json-structural-1";
const __ef_codecTagKey = "_tag";
const __ef_codecMaxDepth = 512;
const __ef_codecI64Min = -(2n ** 63n);
const __ef_codecI64Max = 2n ** 63n - 1n;
class __ef_codecStop {
  constructor(reason, offset, path) { this.reason = reason; this.offset = offset; this.path = path; }
}
const __ef_codecPath = path => {
  const names = [];
  for (let at = path; at !== null; at = at.parent) names.push(at.name);
  return names.reverse();
};
// JSON.stringify's escape for a quote, backslash or C0 control unit.
const __ef_codecEscape = unit => {
  switch (unit) {
    case 0x22: return "\\\"";
    case 0x5c: return "\\\\";
    case 0x08: return "\\b";
    case 0x0c: return "\\f";
    case 0x0a: return "\\n";
    case 0x0d: return "\\r";
    case 0x09: return "\\t";
    default: return "\\u00" + unit.toString(16).padStart(2, "0");
  }
};
const __ef_codecUTF8Length = text => {
  let size = 0;
  for (let i = 0; i < text.length; i++) {
    const unit = text.charCodeAt(i);
    if (unit < 0x80) size += 1;
    else if (unit < 0x800) size += 2;
    else if (unit >= 0xd800 && unit <= 0xdbff && i + 1 < text.length) { size += 4; i++; }
    else size += 3;
  }
  return size;
};

// The single admission pass: RFC 8259 grammar with no byte-order mark,
// well-formed UTF-8, paired surrogate escapes, unique object keys and
// bounded nesting. Failures stop at the byte offset where admission ended.
const __ef_codecParse = (data, maxDepth, text) => {
  let pos = 0, depth = 0;
  const stop = (reason, offset) => { throw new __ef_codecStop(reason, offset, null); };
  const at = c => pos < data.length && data[pos] === c;
  const digit = () => pos < data.length && data[pos] >= 0x30 && data[pos] <= 0x39;
  const space = () => {
    while (pos < data.length && (data[pos] === 0x20 || data[pos] === 0x09 || data[pos] === 0x0a || data[pos] === 0x0d)) pos++;
  };
  const literal = word => {
    for (let i = 0; i < word.length; i++) if (pos + i >= data.length || data[pos + i] !== word.charCodeAt(i)) stop("syntax", pos + i);
    pos += word.length;
  };
  const number = () => {
    if (at(0x2d)) pos++;
    if (!digit()) stop("syntax", pos);
    if (at(0x30)) pos++;
    else while (digit()) pos++;
    if (at(0x2e)) {
      pos++;
      if (!digit()) stop("syntax", pos);
      while (digit()) pos++;
    }
    if (at(0x65) || at(0x45)) {
      pos++;
      if (at(0x2b) || at(0x2d)) pos++;
      if (!digit()) stop("syntax", pos);
      while (digit()) pos++;
    }
  };
  // Mirrors Go's utf8.DecodeRune acceptance; returns the sequence length.
  const sequence = () => {
    const lead = data[pos];
    const continuation = (offset, low = 0x80, high = 0xbf) => pos + offset < data.length && data[pos + offset] >= low && data[pos + offset] <= high;
    let size = 0;
    if (lead >= 0xc2 && lead <= 0xdf) size = continuation(1) ? 2 : 0;
    else if (lead === 0xe0) size = continuation(1, 0xa0) && continuation(2) ? 3 : 0;
    else if (lead === 0xed) size = continuation(1, 0x80, 0x9f) && continuation(2) ? 3 : 0;
    else if (lead >= 0xe1 && lead <= 0xef) size = continuation(1) && continuation(2) ? 3 : 0;
    else if (lead === 0xf0) size = continuation(1, 0x90) && continuation(2) && continuation(3) ? 4 : 0;
    else if (lead === 0xf4) size = continuation(1, 0x80, 0x8f) && continuation(2) && continuation(3) ? 4 : 0;
    else if (lead >= 0xf1 && lead <= 0xf3) size = continuation(1) && continuation(2) && continuation(3) ? 4 : 0;
    if (size === 0) stop("invalid-unicode", pos);
    return size;
  };
  const unicodeEscape = offset => {
    if (offset + 6 > data.length || data[offset] !== 0x5c || data[offset + 1] !== 0x75) return -1;
    let unit = 0;
    for (let i = offset + 2; i < offset + 6; i++) {
      const c = data[i];
      const value = c >= 0x30 && c <= 0x39 ? c - 0x30 : c >= 0x61 && c <= 0x66 ? c - 0x57 : c >= 0x41 && c <= 0x46 ? c - 0x37 : -1;
      if (value < 0) return -1;
      unit = unit * 16 + value;
    }
    return unit;
  };
  const escape = () => {
    const offset = pos;
    if (pos + 1 >= data.length) stop("syntax", offset);
    let simple;
    switch (data[pos + 1]) {
      case 0x22: simple = "\""; break;
      case 0x5c: simple = "\\"; break;
      case 0x2f: simple = "/"; break;
      case 0x62: simple = "\b"; break;
      case 0x66: simple = "\f"; break;
      case 0x6e: simple = "\n"; break;
      case 0x72: simple = "\r"; break;
      case 0x74: simple = "\t"; break;
      case 0x75: {
        const unit = unicodeEscape(pos);
        if (unit < 0) stop("syntax", offset);
        pos += 6;
        if (unit >= 0xdc00 && unit <= 0xdfff) stop("invalid-unicode", offset);
        if (unit < 0xd800 || unit > 0xdbff) return String.fromCharCode(unit);
        const low = unicodeEscape(pos);
        if (low < 0xdc00 || low > 0xdfff) stop("invalid-unicode", offset);
        pos += 6;
        return String.fromCharCode(unit, low);
      }
      default: stop("syntax", offset);
    }
    pos += 2;
    return simple;
  };
  const string = () => {
    pos++;
    let start = pos;
    const parts = [];
    for (;;) {
      if (pos >= data.length) stop("syntax", pos);
      const c = data[pos];
      if (c === 0x22) {
        if (start < pos) parts.push(text.decode(data.subarray(start, pos)));
        pos++;
        return parts.join("");
      }
      if (c === 0x5c) {
        if (start < pos) parts.push(text.decode(data.subarray(start, pos)));
        parts.push(escape());
        start = pos;
      } else if (c < 0x20) stop("syntax", pos);
      else if (c < 0x80) pos++;
      else pos += sequence();
    }
  };
  const enter = () => {
    depth++;
    if (depth > maxDepth) stop("depth", pos);
    pos++;
    space();
  };
  const object = () => {
    enter();
    const members = new Map();
    if (at(0x7d)) { pos++; depth--; return { kind: "object", members }; }
    for (;;) {
      if (!at(0x22)) stop("syntax", pos);
      const keyOffset = pos;
      const key = string();
      if (members.has(key)) stop("duplicate-key", keyOffset);
      space();
      if (!at(0x3a)) stop("syntax", pos);
      pos++;
      space();
      members.set(key, value());
      space();
      if (at(0x2c)) { pos++; space(); continue; }
      if (at(0x7d)) { pos++; depth--; return { kind: "object", members }; }
      stop("syntax", pos);
    }
  };
  const array = () => {
    enter();
    const items = [];
    if (at(0x5d)) { pos++; depth--; return { kind: "array", items }; }
    for (;;) {
      items.push(value());
      space();
      if (at(0x2c)) { pos++; space(); continue; }
      if (at(0x5d)) { pos++; depth--; return { kind: "array", items }; }
      stop("syntax", pos);
    }
  };
  const value = () => {
    if (pos >= data.length) stop("syntax", pos);
    const c = data[pos];
    if (c === 0x7b) return object();
    if (c === 0x5b) return array();
    if (c === 0x22) return { kind: "string", value: string() };
    if (c === 0x74) { literal("true"); return { kind: "bool", value: true }; }
    if (c === 0x66) { literal("false"); return { kind: "bool", value: false }; }
    if (c === 0x6e) { literal("null"); return { kind: "null" }; }
    if (c === 0x2d || (c >= 0x30 && c <= 0x39)) { number(); return { kind: "number" }; }
    stop("syntax", pos);
  };
  space();
  const document = value();
  space();
  if (pos !== data.length) stop("syntax", pos);
  return document;
};

// __ef_codecCompile validates a plan once (linear in nodes and edges) and
// returns { decode(Uint8Array), encode(value) }. Each returns
// { ok: true, value | bytes } or { ok: false, issue }.
const __ef_codecCompile = plan => {
  const invalid = message => { throw new Error("invalid codec plan: " + message); };
  const named = text => typeof text === "string" && text !== "" && text.isWellFormed();
  const index = (value, length) => Number.isSafeInteger(value) && value >= 0 && value < length;
  if (plan?.profile !== __ef_codecProfile) invalid("unsupported profile " + JSON.stringify(plan?.profile));
  const { maxBodyBytes, maxDepth } = plan.bounds ?? {};
  if (!Number.isSafeInteger(maxBodyBytes) || maxBodyBytes < 1) invalid("maxBodyBytes must be positive");
  if (!Number.isSafeInteger(maxDepth) || maxDepth < 1 || maxDepth > __ef_codecMaxDepth) invalid(`maxDepth must be between 1 and ${__ef_codecMaxDepth}`);
  const nodes = plan.nodes;
  if (!Array.isArray(nodes) || nodes.length === 0 || nodes.length > 4096) invalid("plan must have between 1 and 4096 nodes");
  if (!index(plan.root, nodes.length)) invalid(`root ${plan.root} is not a node`);
  const compiled = nodes.map(node => ({ kind: node?.kind }));
  const primitives = new Map(), types = new Map();
  let edges = 0;
  const checkFields = (at, fields, reserved) => {
    if (!Array.isArray(fields)) invalid(`node ${at} needs a field list`);
    const names = new Set();
    for (const field of fields) {
      edges++;
      if (!named(field?.name)) invalid(`node ${at} has an empty or invalid field name`);
      if (field.name === reserved) invalid(`node ${at} variant field ${JSON.stringify(field.name)} collides with the discriminator`);
      if (names.has(field.name)) invalid(`node ${at} repeats field ${JSON.stringify(field.name)}`);
      names.add(field.name);
      if (!index(field.node, nodes.length)) invalid(`node ${at} field ${JSON.stringify(field.name)} references missing node ${field.node}`);
    }
  };
  nodes.forEach((node, at) => {
    switch (node?.kind) {
      case "string": case "bool": case "void": case "i64":
        if (node.type !== undefined || node.fields !== undefined || node.variants !== undefined) invalid(`primitive node ${at} carries nominal structure`);
        if (primitives.has(node.kind)) invalid(`nodes ${primitives.get(node.kind)} and ${at} repeat primitive ${node.kind} instead of sharing it`);
        primitives.set(node.kind, at);
        break;
      case "record": case "union":
        if (!named(node.type)) invalid(`nominal node ${at} needs a type identity`);
        if (types.has(node.type)) invalid(`nodes ${types.get(node.type)} and ${at} repeat type ${JSON.stringify(node.type)} instead of sharing it`);
        types.set(node.type, at);
        break;
      default:
        invalid(`node ${at} has unsupported kind ${JSON.stringify(node?.kind)}`);
    }
    if (node.kind === "record") {
      if (node.variants !== undefined) invalid(`record node ${at} has variants`);
      checkFields(at, node.fields, undefined);
    } else if (node.kind === "union") {
      if (node.fields !== undefined || !Array.isArray(node.variants) || node.variants.length === 0) invalid(`union node ${at} needs variants and no record fields`);
      const tags = new Map(), domain = new Map();
      node.variants.forEach((variant, position) => {
        edges++;
        if (!named(variant?.tag)) invalid(`union node ${at} has an empty or invalid tag`);
        if (tags.has(variant.tag)) invalid(`union node ${at} repeats tag ${JSON.stringify(variant.tag)}`);
        if (!named(variant.domainTag) || domain.has(variant.domainTag)) invalid(`union node ${at} needs a distinct domain tag for ${JSON.stringify(variant.tag)}`);
        tags.set(variant.tag, position);
        domain.set(variant.domainTag, position);
        checkFields(at, variant.fields, __ef_codecTagKey);
      });
      Object.assign(compiled[at], { tags, domain });
    }
    if (edges > 65536) invalid("more than 65536 fields and variants");
  });
  const link = fields => fields.map(field => ({ name: field.name, key: JSON.stringify(field.name) + ":", node: compiled[field.node] }));
  nodes.forEach((node, at) => {
    if (node.kind === "record") compiled[at].fields = link(node.fields);
    if (node.kind === "union") {
      compiled[at].variants = node.variants.map(variant => ({
        domainTag: variant.domainTag, open: "{" + JSON.stringify(__ef_codecTagKey) + ":" + JSON.stringify(variant.tag), fields: link(variant.fields),
      }));
    }
  });
  // Depth-first validation with memoized depths: each node and edge once.
  const state = nodes.map(() => 0), depths = nodes.map(() => 0);
  const visit = (at, containers) => {
    if (state[at] === 2) return;
    if (state[at] === 1) invalid(`node ${at} is part of a recursive layout`);
    const node = nodes[at];
    const container = node.kind === "record" || node.kind === "union";
    if (container && ++containers > maxDepth) invalid(`plan nesting exceeds maxDepth ${maxDepth}`);
    state[at] = 1;
    let deepest = 0;
    const children = [...(node.fields ?? []), ...(node.variants ?? []).flatMap(variant => variant.fields)];
    for (const child of children) {
      visit(child.node, containers);
      deepest = Math.max(deepest, depths[child.node]);
    }
    depths[at] = deepest + (container ? 1 : 0);
    state[at] = 2;
  };
  visit(plan.root, 0);
  if (depths[plan.root] > maxDepth) invalid(`plan nesting exceeds maxDepth ${maxDepth}`);
  state.forEach((visited, at) => { if (visited !== 2) invalid(`node ${at} is unreachable from root`); });

  const root = compiled[plan.root];
  const text = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true });
  const bytes = new TextEncoder();
  const fail = (reason, path) => { throw new __ef_codecStop(reason, -1, path); };
  const settle = (direction, run) => {
    try {
      return run();
    } catch (stop) {
      if (!(stop instanceof __ef_codecStop)) throw stop;
      return { ok: false, issue: { direction, reason: stop.reason, path: __ef_codecPath(stop.path), offset: stop.offset } };
    }
  };
  const parseI64 = (digits, path) => {
    const negative = digits.startsWith("-");
    if (!/^[0-9]+$/.test(negative ? digits.slice(1) : digits)) fail("integer", path);
    const trimmed = (negative ? digits.slice(1) : digits).replace(/^0+/, "");
    if (trimmed === "") return 0n;
    if (trimmed.length > 19) fail("range", path);
    const number = BigInt(negative ? "-" + trimmed : trimmed);
    if (number < __ef_codecI64Min || number > __ef_codecI64Max) fail("range", path);
    return number;
  };
  const decodeFields = (fields, object, path) => fields.map(field => {
    const at = { parent: path, name: field.name };
    const member = object.members.get(field.name);
    if (member === undefined) fail("missing", at);
    return [field.name, decodeNode(field.node, member, at)];
  });
  const decodeNode = (node, value, path) => {
    switch (node.kind) {
      case "string": if (value.kind === "string") return value.value; break;
      case "bool": if (value.kind === "bool") return value.value; break;
      case "void": if (value.kind === "null") return undefined; break;
      case "i64": if (value.kind === "string") return parseI64(value.value, path); break;
      case "record": if (value.kind === "object") return Object.fromEntries(decodeFields(node.fields, value, path)); break;
      case "union":
        if (value.kind === "object") {
          const tag = value.members.get(__ef_codecTagKey);
          if (tag === undefined || tag.kind !== "string" || !node.tags.has(tag.value)) fail("tag", path);
          const variant = node.variants[node.tags.get(tag.value)];
          return Object.fromEntries([[__ef_codecTagKey, variant.domainTag], ...decodeFields(variant.fields, value, path)]);
        }
        break;
    }
    fail("type", path);
  };
  const decode = body => {
    if (!(body instanceof Uint8Array)) throw new TypeError("effra codec: decode expects a Uint8Array");
    return settle("decode", () => {
      if (body.length > maxBodyBytes) fail("body-too-large", null);
      return { ok: true, value: decodeNode(root, __ef_codecParse(body, maxDepth, text), null) };
    });
  };
  const encode = value => settle("encode", () => {
    // Every byte is reserved against the allowance before the piece holding
    // it is built, so no piece or output beyond maxBodyBytes is constructed.
    // Failures surface in encode order; a string's Unicode is validated
    // before any of its bytes are written, as in the Go engine.
    const out = [];
    let size = 0;
    const reserve = count => {
      size += count;
      if (size > maxBodyBytes) fail("body-too-large", null);
    };
    const write = piece => {
      reserve(__ef_codecUTF8Length(piece));
      out.push(piece);
    };
    // Writes well-formed text with JSON.stringify's escaping. Unescaped runs
    // are sliced from the input only after each unit's UTF-8 size is reserved.
    const writeString = text => {
      write("\"");
      let start = 0;
      for (let i = 0; i < text.length; i++) {
        const unit = text.charCodeAt(i);
        if (unit < 0x20 || unit === 0x22 || unit === 0x5c) {
          if (start < i) out.push(text.slice(start, i));
          start = i + 1;
          write(__ef_codecEscape(unit));
        } else if (unit < 0x80) reserve(1);
        else if (unit < 0x800) reserve(2);
        else if (unit >= 0xd800 && unit <= 0xdbff) { reserve(4); i++; }
        else reserve(3);
      }
      if (start < text.length) out.push(text.slice(start));
      write("\"");
    };
    const defect = message => { throw new Error("effra codec: " + message); };
    const encodeFields = (fields, object, path, separated) => {
      for (const field of fields) {
        if (!Object.hasOwn(object, field.name)) defect(`carrier lacks field ${JSON.stringify(field.name)}`);
        if (separated) write(",");
        separated = true;
        write(field.key);
        encodeNode(field.node, object[field.name], { parent: path, name: field.name });
      }
    };
    const encodeNode = (node, value, path) => {
      switch (node.kind) {
        case "string":
          if (typeof value !== "string") defect(`string node received ${typeof value}`);
          if (!value.isWellFormed()) fail("invalid-unicode", path);
          return writeString(value);
        case "bool":
          if (typeof value !== "boolean") defect(`bool node received ${typeof value}`);
          return write(value ? "true" : "false");
        case "void":
          if (value !== undefined) defect(`void node received ${typeof value}`);
          return write("null");
        case "i64":
          if (typeof value !== "bigint") defect(`i64 node received ${typeof value}`);
          if (value < __ef_codecI64Min || value > __ef_codecI64Max) fail("range", path);
          return write("\"" + value.toString() + "\"");
        case "record":
          if (typeof value !== "object" || value === null) defect(`record node received ${typeof value}`);
          write("{");
          encodeFields(node.fields, value, path, false);
          return write("}");
        case "union": {
          const position = typeof value === "object" && value !== null ? node.domain.get(value[__ef_codecTagKey]) : undefined;
          if (position === undefined) defect("union carrier has an unknown domain tag");
          const variant = node.variants[position];
          write(variant.open);
          encodeFields(variant.fields, value, path, true);
          return write("}");
        }
      }
    };
    encodeNode(root, value, null);
    return { ok: true, bytes: bytes.encode(out.join("")) };
  });
  return { decode, encode };
};
