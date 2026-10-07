// Managed HTTP/1.1 transport over node:http (Node and Bun). The contract is
// runtime/effra/http.go's: explicit limits, buffered bodies, request scopes
// linked to client disconnect and server shutdown, and publication only
// after each request scope has closed. node:http loads on first listen, so
// programs that never serve pay nothing for it.
const __ef_http_max_millis = 2147483647n;
const __ef_http_max_count = 9007199254740991n;
const __ef_http_limits = limits => {
  for (const millis of [limits.readHeaderMillis, limits.readBodyMillis, limits.idleMillis]) {
    if (millis < 1n || millis > __ef_http_max_millis) throw new Error('invalid HTTP limits: timeouts must be within 1..' + __ef_http_max_millis + ' milliseconds');
  }
  if (limits.maxBodyBytes < 0n || limits.maxBodyBytes > __ef_http_max_count || limits.maxActive < 1n || limits.maxActive > __ef_http_max_millis) {
    throw new Error('invalid HTTP limits: body ' + limits.maxBodyBytes + ' bytes, active ' + limits.maxActive);
  }
  return { maxBodyBytes: Number(limits.maxBodyBytes), readHeaderMillis: Number(limits.readHeaderMillis), readBodyMillis: Number(limits.readBodyMillis), idleMillis: Number(limits.idleMillis), maxActive: Number(limits.maxActive) };
};
// The path component of the request target exactly as received.
const __ef_http_path = target => {
  if (!target.startsWith('/')) {
    const scheme = target.indexOf('://');
    if (scheme > 0) {
      const rest = target.slice(scheme + 3);
      const slash = rest.indexOf('/');
      if (slash < 0) return '/';
      target = rest.slice(slash);
    }
  }
  const query = target.indexOf('?');
  return query < 0 ? target : target.slice(0, query);
};
const __ef_http_status = (res, status, close) => {
  if (res.headersSent || res.destroyed) return;
  const headers = { 'Content-Length': '0' };
  if (close) headers.Connection = 'close';
  res.writeHead(status, headers);
  res.end();
};
const __ef_http_publish = (res, response) => {
  const status = response?.status;
  const body = response?.body;
  const contentType = response?.contentType ?? '';
  if (typeof status !== 'bigint' || status < 200n || status > 599n || !(body instanceof Uint8Array) || ((status === 204n || status === 304n) && body.length > 0) || /[\r\n]/.test(contentType)) {
    __ef_http_status(res, 500, false);
    return;
  }
  const headers = { 'Content-Length': String(body.length) };
  if (contentType !== '') headers['Content-Type'] = contentType;
  res.writeHead(Number(status), headers);
  res.end(body);
};
const __ef_http_address = address => {
  const colon = address.lastIndexOf(':');
  if (colon < 0) throw new Error('invalid HTTP address ' + address);
  let host = address.slice(0, colon);
  if (host.startsWith('[') && host.endsWith(']')) host = host.slice(1, -1);
  return { host: host === '' ? undefined : host, port: Number(address.slice(colon + 1)) };
};
const __ef_http_bound = server => {
  const bound = server.address();
  const host = bound.family === 'IPv6' || bound.family === 6 ? '[' + bound.address + ']' : bound.address;
  return host + ':' + bound.port;
};

// __ef_http_serve owns one listener. onRequest(req, res, transport) handles one
// request. transport.run executes an Effra request program in its own owned
// scope with the listener's services and passes its exit to respond unless the
// client is gone; release runs as soon as the response is published or
// abandoned. Shutdown waits for every tracked promise.
const __ef_http_serve = (address, timeouts, onRequest) => Effect.gen(function* () {
  const { createServer } = yield* Effect.promise(() => import('node:http'));
  const context = yield* Effect.context();
  const { host, port } = __ef_http_address(address);
  const transport = { closing: false, admitted: 0, fibers: new Set(), pending: new Set(), reading: new WeakMap() };
  transport.track = settled => {
    transport.pending.add(settled);
    settled.then(() => transport.pending.delete(settled));
  };
  transport.run = (res, program, respond, release = () => {}) => {
    const fiber = Effect.runForkWith(context)(__ef_scoped(program));
    transport.fibers.add(fiber);
    transport.track(new Promise(resolve => {
      const disconnect = () => { if (!res.writableFinished) fiber.interruptUnsafe(); };
      res.once('close', disconnect);
      fiber.addObserver(exit => {
        transport.fibers.delete(fiber);
        res.off('close', disconnect);
        // The client is gone: no response remains possible.
        if (res.destroyed || res.socket?.destroyed) res.destroy();
        else respond(exit);
        release();
        if (res.writableFinished || res.destroyed) {
          resolve();
        } else {
          res.once('finish', resolve);
          res.once('close', resolve);
        }
      });
    }));
  };
  // Header timeouts are checked at this interval rather than node's 30s default.
  const server = createServer({ connectionsCheckingInterval: Math.min(timeouts.readHeaderMillis, 1000) }, (req, res) => onRequest(req, res, transport));
  // Parser failures belong to the transport: release the request's admission
  // before answering, as Go does. A header timeout closes silently.
  server.on('clientError', (error, socket) => {
    transport.reading.get(socket)?.();
    if (error?.code === 'ERR_HTTP_REQUEST_TIMEOUT' || !socket.writable) socket.destroy();
    else socket.end('HTTP/1.1 ' + (error?.code === 'HPE_HEADER_OVERFLOW' ? '431 Request Header Fields Too Large' : '400 Bad Request') + '\r\nContent-Length: 0\r\nConnection: close\r\n\r\n');
  });
  server.headersTimeout = timeouts.readHeaderMillis;
  server.requestTimeout = 0;
  server.keepAliveTimeout = timeouts.idleMillis;
  const listen = Effect.callback(resume => {
    const failed = error => resume(Effect.fail({ _tag: 'IoError', message: String(error?.message ?? error) }));
    server.once('error', failed);
    server.listen(port, host, () => { server.off('error', failed); resume(Effect.void); });
  });
  // Shutdown stops admission, cancels every active request, waits for each
  // request scope and its published response, then closes the listener.
  const shutdown = Effect.promise(async () => {
    transport.closing = true;
    const closed = new Promise(resolve => server.close(() => resolve()));
    for (const fiber of transport.fibers) fiber.interruptUnsafe();
    while (transport.pending.size > 0) await Promise.all([...transport.pending]);
    server.closeAllConnections?.();
    await closed;
  });
  return yield* Effect.acquireUseRelease(listen, () => Effect.gen(function* () {
    console.log('listening http://' + __ef_http_bound(server));
    return yield* Effect.never;
  }), () => shutdown);
});

// The raw path-to-text transport control.
const __ef_http_serve_text = (address, handler) => __ef_http_serve(address, { readHeaderMillis: 5000, idleMillis: 5000 }, (req, res, transport) => {
  let path;
  try { path = decodeURIComponent(new URL(req.url, 'http://localhost').pathname); } catch { __ef_http_status(res, 400, true); return; }
  req.resume();
  transport.run(res, handler(path), exit => {
    const failed = Exit.isFailure(exit);
    const body = Buffer.from(failed ? 'Internal Server Error\n' : String(exit.value));
    const headers = { 'Content-Type': 'text/plain; charset=utf-8', 'Content-Length': String(body.length) };
    if (failed) headers['X-Content-Type-Options'] = 'nosniff';
    res.writeHead(failed ? 500 : 200, headers);
    res.end(body);
  });
});

const __ef_http_listen = (address, source, handler) => Effect.suspend(() => {
  let limits;
  try { limits = __ef_http_limits(source); } catch (error) { return Effect.die(error); }
  return __ef_http_serve(address, limits, (req, res, transport) => {
    if (transport.closing || transport.admitted >= limits.maxActive) { __ef_http_status(res, 503, true); return; }
    const declared = req.headers['content-length'];
    if (declared !== undefined && Number(declared) > limits.maxBodyBytes) { __ef_http_status(res, 413, true); return; }
    // Admission covers the body read, so buffered bodies are bounded too.
    transport.admitted++;
    let released = false;
    const release = () => { if (!released) { released = true; transport.admitted--; } };
    const reading = { done: false, chunks: [], size: 0 };
    transport.track(new Promise(resolve => { reading.settle = resolve; }));
    const finish = () => { reading.done = true; clearTimeout(timer); transport.reading.delete(req.socket); reading.settle(); };
    const abort = () => { if (reading.done) return; finish(); release(); req.socket?.destroy(); };
    const timer = setTimeout(abort, limits.readBodyMillis);
    transport.reading.set(req.socket, () => { if (reading.done) return; finish(); release(); });
    req.on('data', chunk => {
      if (reading.done) return;
      reading.size += chunk.length;
      if (reading.size > limits.maxBodyBytes) { finish(); release(); __ef_http_status(res, 413, true); req.resume(); return; }
      reading.chunks.push(chunk);
    });
    req.on('error', () => { if (reading.done) return; finish(); release(); __ef_http_status(res, 400, true); });
    req.on('close', abort);
    req.on('end', () => {
      if (reading.done) return;
      finish();
      if (transport.closing) { release(); __ef_http_status(res, 503, true); return; }
      const request = { method: req.method, path: __ef_http_path(req.url), contentType: req.headers['content-type'] ?? '', body: new Uint8Array(Buffer.concat(reading.chunks)) };
      transport.run(res, handler(request), exit => {
        if (Exit.isSuccess(exit)) __ef_http_publish(res, __ef_http_reply(exit.value));
        else if (transport.closing && Cause.hasInterrupts(exit.cause)) __ef_http_status(res, 503, true);
        else __ef_http_status(res, 500, transport.closing);
      }, release);
    });
  });
});

const __ef_http_empty = new Uint8Array(0);
const __ef_http_reply = reply => {
  switch (reply?._tag) {
    case 'HttpReply.Respond': return reply.response;
    case 'HttpReply.BadRequest': return { status: 400n, contentType: '', body: __ef_http_empty };
    case 'HttpReply.NotFound': return { status: 404n, contentType: '', body: __ef_http_empty };
    case 'HttpReply.UnsupportedMediaType': return { status: 415n, contentType: '', body: __ef_http_empty };
  }
  return undefined;
};
const __ef_provider_LiveHttp = {
  serve: (address, handler) => __ef_http_serve_text(address, handler),
  listen: (address, limits, handler) => __ef_http_listen(address, limits, handler),
  text: text => Effect.sync(() => new TextEncoder().encode(text)),
};
