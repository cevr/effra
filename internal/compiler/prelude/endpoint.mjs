// The buffered JSON endpoint profile shared by every derived endpoint; see
// goEndpointHelpers for the policy, which both targets implement alike.
const __ef_endpointText = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true });
const __ef_endpointBytes = new TextEncoder();
const __ef_endpointJSON = contentType => contentType === "" || contentType.split(";")[0].replace(/^[ \t]+|[ \t]+$/g, "").toLowerCase() === "application/json";
const __ef_endpointRespond = (status, body) => Effect.succeed({ _tag: "HttpReply.Respond", response: body === undefined ? { status, contentType: "", body: new Uint8Array(0) } : { status, contentType: "application/json", body: __ef_endpointBytes.encode(body) } });
const __ef_endpointOnly = (cause, tag) => cause.reasons.length === 1 && cause.reasons[0]._tag === "Fail" && cause.reasons[0].error?._tag === tag;
const __ef_endpointFailed = (cause, failures) => {
  const failure = cause.reasons.length === 1 && cause.reasons[0]._tag === "Fail" ? cause.reasons[0].error : undefined;
  const mapped = failure === undefined ? undefined : failures.get(failure._tag);
  if (mapped === undefined) return Effect.failCause(cause);
  if (mapped.encode === undefined) return __ef_endpointRespond(mapped.status);
  return Effect.flatMap(Effect.exit(mapped.encode(failure)), exit => Exit.isSuccess(exit) ? __ef_endpointRespond(mapped.status, exit.value) : Effect.failCause(Cause.combine(exit.cause, cause)));
};
const __ef_endpointStage = (program, failures, next) => Effect.flatMap(Effect.exit(program), exit => Exit.isSuccess(exit) ? next(exit.value) : __ef_endpointFailed(exit.cause, failures));
const __ef_endpoint = (request, method, path, decode, handler, encode, status, failures) => Effect.suspend(() => {
  if (request.method !== method || request.path !== path) return Effect.succeed({ _tag: "HttpReply.NotFound" });
  const run = input => __ef_endpointStage(handler(input), failures, result => encode === undefined ? __ef_endpointRespond(status) : __ef_endpointStage(encode(result), failures, body => __ef_endpointRespond(status, body)));
  if (decode === undefined) return run(undefined);
  if (!__ef_endpointJSON(request.contentType)) return Effect.succeed({ _tag: "HttpReply.UnsupportedMediaType" });
  let text;
  try {
    text = __ef_endpointText.decode(request.body);
  } catch {
    // Not UTF-8: the Go engine reports the same body as malformed input.
    return Effect.succeed({ _tag: "HttpReply.BadRequest" });
  }
  return Effect.flatMap(Effect.exit(decode(text)), exit => Exit.isSuccess(exit) ? run(exit.value) : __ef_endpointOnly(exit.cause, "JsonDecodeFailure") ? Effect.succeed({ _tag: "HttpReply.BadRequest" }) : __ef_endpointFailed(exit.cause, failures));
});
const __ef_routes = (request, table) => Effect.suspend(() => {
  const key = request.method + " " + request.path;
  const route = table.get(key);
  return route === undefined ? Effect.succeed({ _tag: "HttpReply.NotFound" }) : route(request);
});
