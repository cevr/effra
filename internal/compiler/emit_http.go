package compiler

import (
	"strconv"
	"strings"
)

// goHTTPProvider adapts the builtin Http data prelude to the native managed
// transport. Field and variant selectors come from the ordinary data
// lowering, so this adapter cannot drift from the emitted declarations.
func goHTTPProvider() string {
	field := goFieldName
	reply := func(variant string) string { return goVariantType("HttpReply", variant) }
	replacer := strings.NewReplacer(
		"$method", field("method"), "$path", field("path"), "$contentType", field("contentType"), "$body", field("body"),
		"$status", field("status"), "$response", field("response"),
		"$maxBodyBytes", field("maxBodyBytes"), "$readHeaderMillis", field("readHeaderMillis"), "$readBodyMillis", field("readBodyMillis"), "$idleMillis", field("idleMillis"), "$maxActive", field("maxActive"),
		"$Respond", reply("Respond"), "$BadRequest", reply("BadRequest"), "$NotFound", reply("NotFound"), "$UnsupportedMediaType", reply("UnsupportedMediaType"),
	)
	return replacer.Replace(`func efHttpResponse(reply efType_HttpReply)er.HTTPResponse{switch value:=reply.(type){case $Respond:return er.HTTPResponse{Status:int(value.$response.$status),ContentType:value.$response.$contentType,Body:value.$response.$body};case $BadRequest:return er.HTTPResponse{Status:400};case $NotFound:return er.HTTPResponse{Status:404};case $UnsupportedMediaType:return er.HTTPResponse{Status:415}};return er.HTTPResponse{}}
func efProvider_LiveHttp()efService_Http{return efService_Http{m_listen:func(address string,limits efType_HttpLimits,handler func(efType_HttpRequest)efEffect[efType_HttpReply])efEffect[struct{}]{return func(ctx efContext)efExit[struct{}]{bounds,err:=er.HTTPLimitsFromMillis(limits.$maxBodyBytes,limits.$readHeaderMillis,limits.$readBodyMillis,limits.$idleMillis,limits.$maxActive);if err!=nil{return er.Die[struct{}](err)};return er.Invoke(ctx.Runtime,er.ServeHTTPRequests(address,bounds,func(request er.HTTPRequest)er.Effect[er.HTTPResponse]{return func(fc *er.FiberContext)er.Exit[er.HTTPResponse]{exit:=er.Invoke(fc,efToRuntime(ctx,handler(efType_HttpRequest{$method:request.Method,$path:request.Path,$contentType:request.ContentType,$body:request.Body})));if exit.IsFailure(){return er.Propagate[er.HTTPResponse](exit)};return er.Succeed(efHttpResponse(exit.Value))}},func(bound string){fmt.Println("listening http://"+bound)}))}},m_text:func(text string)efEffect[[]byte]{return func(efContext)efExit[[]byte]{return er.Succeed([]byte(text))}}}}
`)
}

// endpointLoweringHelper names the lowering helpers that serve a derived
// endpoint or route table. The application plan retains them only through
// a reachable endpoint operation.
const endpointLoweringHelper = "endpoint"

// goEndpointHelpers implement the buffered JSON endpoint profile once for
// every endpoint, over the ordinary Http data lowering:
//
//   - a request for another method or path answers NotFound, so a route
//     table's wrong method answers 404, the pinned Effect profile;
//   - an endpoint with input requires the application/json media type
//     (parameters admitted, case-insensitive; an absent Content-Type is
//     read as JSON, as pinned Effect's HttpApiBuilder does) or answers
//     UnsupportedMediaType, then decodes the body once with its input
//     witness; that witness's structural JsonDecodeFailure answers
//     BadRequest with an empty body;
//   - success encodes the handler's result with the output witness at the
//     declared status, and a status without output has no body;
//   - a mapped named failure answers its status, with a body encoded by its
//     failure witness if it names one. A failure is mapped only when it is
//     the whole cause: a defect, interruption or cleanup cause beside it
//     propagates unchanged;
//   - a failed failure-body encoding is terminal: it is never mapped again,
//     and its cause keeps the mapped failure after the encoding failure, so
//     the transport answers its generic 500 without losing either.
//
// The checker guarantees that JsonDecodeFailure reaches here only from the
// input witness's structure and JsonEncodeFailure only from response
// encoding: neither is mappable, and a handler or transformation raising
// either must recover it.
func goEndpointHelpers() string {
	field := goFieldName
	reply := func(variant string) string { return goVariantType("HttpReply", variant) }
	replacer := strings.NewReplacer(
		"$method", field("method"), "$path", field("path"), "$contentType", field("contentType"), "$body", field("body"),
		"$status", field("status"), "$response", field("response"),
		"$Respond", reply("Respond"), "$BadRequest", reply("BadRequest"), "$NotFound", reply("NotFound"), "$UnsupportedMediaType", reply("UnsupportedMediaType"),
	)
	return replacer.Replace(`type efEndpointFailure struct{status int64;encode func(any)efExit[string]}
func efEndpointJSON(contentType string)bool{if contentType==""{return true};media:=contentType;for i:=0;i<len(media);i++{if media[i]==';'{media=media[:i];break}};start,end:=0,len(media);for start<end&&(media[start]==' '||media[start]=='\t'){start++};for end>start&&(media[end-1]==' '||media[end-1]=='\t'){end--};media=media[start:end];const want="application/json";if len(media)!=len(want){return false};for i:=0;i<len(want);i++{c:=media[i];if c>='A'&&c<='Z'{c+='a'-'A'};if c!=want[i]{return false}};return true}
func efEndpointRespond(status int64,body []byte,json bool)efExit[efType_HttpReply]{contentType:="";if json{contentType="application/json"};return er.Succeed[efType_HttpReply]($Respond{$response:efType_HttpResponse{$status:status,$contentType:contentType,$body:body}})}
func efEndpointOnly[A any](exit efExit[A],tag string)bool{return exit.Failure!=nil&&exit.Defect==nil&&!exit.Interrupted&&len(exit.Additional)==0&&exit.Failure.Tag==tag}
func efEndpointFailed[A any](exit efExit[A],failures map[string]efEndpointFailure)efExit[efType_HttpReply]{if exit.Failure!=nil&&efEndpointOnly(exit,exit.Failure.Tag){if mapped,ok:=failures[exit.Failure.Tag];ok{if mapped.encode==nil{return efEndpointRespond(mapped.status,nil,false)};body:=mapped.encode(exit.Failure.Payload);if body.IsFailure(){return er.FromCause[efType_HttpReply](append(body.Cause(),exit.Cause()...))};return efEndpointRespond(mapped.status,[]byte(body.Value),true)}};return er.Propagate[efType_HttpReply](exit)}
func efEndpoint[I,O any](ctx efContext,request efType_HttpRequest,method,path string,decode func(string)efEffect[I],handler func(efContext,I)efExit[O],encode func(O)efEffect[string],status int64,failures map[string]efEndpointFailure)efExit[efType_HttpReply]{if request.$method!=method||request.$path!=path{return er.Succeed[efType_HttpReply]($NotFound{})};var input I;if decode!=nil{if !efEndpointJSON(request.$contentType){return er.Succeed[efType_HttpReply]($UnsupportedMediaType{})};decoded:=decode(string(request.$body))(ctx);if efEndpointOnly(decoded,"JsonDecodeFailure"){return er.Succeed[efType_HttpReply]($BadRequest{})};if decoded.IsFailure(){return efEndpointFailed(decoded,failures)};input=decoded.Value};result:=handler(ctx,input);if result.IsFailure(){return efEndpointFailed(result,failures)};if encode==nil{return efEndpointRespond(status,nil,false)};encoded:=encode(result.Value)(ctx);if encoded.IsFailure(){return efEndpointFailed(encoded,failures)};return efEndpointRespond(status,[]byte(encoded.Value),true)}
func efRoutes(ctx efContext,request efType_HttpRequest,table map[string]func(efType_HttpRequest)efEffect[efType_HttpReply])efExit[efType_HttpReply]{if endpoint,ok:=table[request.$method+" "+request.$path];ok{return endpoint(request)(ctx)};return er.Succeed[efType_HttpReply]($NotFound{})}
`)
}

// endpointOperation lowers a synthesized endpoint or route table body. An
// endpoint adapts its handler to one context-taking step from the input
// domain value (struct{} without input) to the output domain value
// (struct{} without output); a failure witness encodes the failure's
// payload, which is its declaration's struct or nil for a fieldless one.
func (g *goEmitter) endpointOperation(e *Expr, ret string, out *strings.Builder) string {
	c := g.program.semantic
	endpoint := e.endpoint
	request := g.expr(e.Left, true, ret, out)
	name := g.temp()
	if e.Kind == "routes" {
		entries := []string{}
		for _, route := range endpoint.endpoints {
			entries = append(entries, strconv.Quote(route.method+" "+route.path)+":"+route.function.goEmissionName())
		}
		out.WriteString(name + " := efRoutes(ctx," + request + ",map[string]func(efType_HttpRequest)efEffect[efType_HttpReply]{" + strings.Join(entries, ",") + "})\n" + g.failed(name, ret))
		return name + ".Value"
	}
	goType := func(codec *CodecDeclaration) string {
		if codec == nil {
			return "struct{}"
		}
		return canonicalGoType(c, codec.domainID, map[TypeID]bool{})
	}
	input, output := goType(endpoint.input), goType(endpoint.output)
	handler := endpoint.handler
	argument := ""
	if endpoint.input != nil {
		argument = "value"
	}
	call := handler.goEmissionName() + "(" + argument + ")"
	var body string
	switch {
	case handler.Effect:
		body = "return " + call + "(ctx)"
	case endpoint.output != nil:
		body = "return er.Succeed(" + call + ")"
	default:
		body = call + ";return er.Succeed(struct{}{})"
	}
	decode, encode := "nil", "nil"
	if endpoint.input != nil {
		decode = endpoint.input.decode.goEmissionName()
	}
	if endpoint.output != nil {
		encode = endpoint.output.encode.goEmissionName()
	}
	failures := []string{}
	for _, mapping := range endpoint.mappings {
		encoder := "nil"
		if mapping.codec != nil {
			encoder = "func(payload any)efExit[string]{encoded,err:=" + goCodecEncodeName(mapping.codec.plan) + "(&payload," + goCodecStepsName(mapping.codec.plan) + "{});return efCodecEncoded(" + strconv.Quote(jsonCodecDerivation.EncodeFailure) + ",encoded,err)}"
		}
		failures = append(failures, strconv.Quote(mapping.failure)+":{status:"+strconv.FormatInt(mapping.status, 10)+",encode:"+encoder+"}")
	}
	out.WriteString(name + " := efEndpoint[" + input + "," + output + "](ctx," + request + "," + strconv.Quote(endpoint.method) + "," + strconv.Quote(endpoint.path) + "," + decode + ",func(ctx efContext,value " + input + ")efExit[" + output + "]{" + body + "}," + encode + "," + strconv.FormatInt(endpoint.status, 10) + ",map[string]efEndpointFailure{" + strings.Join(failures, ",") + "})\n" + g.failed(name, ret))
	return name + ".Value"
}

// jsEndpointOperation lowers a synthesized endpoint or route table body to
// the endpoint prelude. The handler is adapted to a function from its input
// (undefined without input) to an Effect; a failure witness encodes the
// failure carrier itself, which is tagged with the failure's name. Failure
// mappings and routes are Maps keyed by source names, never object
// literals, so a legal failure named __proto__ is an ordinary key.
func jsEndpointOperation(e *Expr) string {
	endpoint := e.endpoint
	request := jsExpr(e.Left, true)
	if e.Kind == "routes" {
		entries := []string{}
		for _, route := range endpoint.endpoints {
			entries = append(entries, "["+quoted(route.method+" "+route.path)+", "+route.function.jsEmissionName()+"]")
		}
		return "(yield* __ef_routes(" + request + ", new Map([" + strings.Join(entries, ", ") + "])))"
	}
	argument := ""
	if endpoint.input != nil {
		argument = "value"
	}
	call := endpoint.handler.jsEmissionName() + "(" + argument + ")"
	if !endpoint.handler.Effect {
		call = "Effect.sync(() => " + call + ")"
	}
	decode, encode := "undefined", "undefined"
	if endpoint.input != nil {
		decode = endpoint.input.decode.jsEmissionName()
	}
	if endpoint.output != nil {
		encode = endpoint.output.encode.jsEmissionName()
	}
	failures := []string{}
	for _, mapping := range endpoint.mappings {
		entry := "[" + quoted(mapping.failure) + ", { status: " + strconv.FormatInt(mapping.status, 10) + "n"
		if mapping.codec != nil {
			entry += ", encode: failure => __ef_codecEncode(" + jsCodecPlanName(mapping.codec.plan) + ", " + quoted(jsonCodecDerivation.EncodeFailure) + ", failure, [])"
		}
		failures = append(failures, entry+" }]")
	}
	return "(yield* __ef_endpoint(" + request + ", " + quoted(endpoint.method) + ", " + quoted(endpoint.path) + ", " + decode + ", (value => " + call + "), " + encode + ", " + strconv.FormatInt(endpoint.status, 10) + "n, new Map([" + strings.Join(failures, ", ") + "])))"
}
