package compiler

import "strings"

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
func efProvider_LiveHttp()efService_Http{return efService_Http{m_serve:func(address string,handler func(string)efEffect[string])efEffect[struct{}]{return func(ctx efContext)efExit[struct{}]{return er.Invoke(ctx.Runtime,er.ServeHTTP(address,func(path string)er.Effect[string]{return efToRuntime(ctx,handler(path))},func(bound string){fmt.Println("listening http://"+bound)}))}},m_listen:func(address string,limits efType_HttpLimits,handler func(efType_HttpRequest)efEffect[efType_HttpReply])efEffect[struct{}]{return func(ctx efContext)efExit[struct{}]{bounds,err:=er.HTTPLimitsFromMillis(limits.$maxBodyBytes,limits.$readHeaderMillis,limits.$readBodyMillis,limits.$idleMillis,limits.$maxActive);if err!=nil{return er.Die[struct{}](err)};return er.Invoke(ctx.Runtime,er.ServeHTTPRequests(address,bounds,func(request er.HTTPRequest)er.Effect[er.HTTPResponse]{return func(fc *er.FiberContext)er.Exit[er.HTTPResponse]{exit:=er.Invoke(fc,efToRuntime(ctx,handler(efType_HttpRequest{$method:request.Method,$path:request.Path,$contentType:request.ContentType,$body:request.Body})));if exit.IsFailure(){return er.Propagate[er.HTTPResponse](exit)};return er.Succeed(efHttpResponse(exit.Value))}},func(bound string){fmt.Println("listening http://"+bound)}))}},m_text:func(text string)efEffect[[]byte]{return func(efContext)efExit[[]byte]{return er.Succeed([]byte(text))}}}}
`)
}
