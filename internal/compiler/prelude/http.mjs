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
// The path component of the request target exactly as received. The query
// is removed first, so a slash inside it never becomes the path.
const __ef_http_path = target => {
  const query = target.indexOf('?');
  if (query >= 0) target = target.slice(0, query);
  if (!target.startsWith('/')) {
    const scheme = target.indexOf('://');
    if (scheme > 0) {
      const rest = target.slice(scheme + 3);
      const slash = rest.indexOf('/');
      return slash < 0 ? '/' : rest.slice(slash);
    }
  }
  return target;
};
// Header values every target publishes unchanged: visible ASCII, space and
// horizontal tab (runtime/effra/http.go admissibleHeaderValue).
const __ef_http_header_value = value => typeof value === 'string' && /^[\t\x20-\x7e]*$/.test(value);
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
  if (typeof status !== 'bigint' || status < 200n || status > 599n || !(body instanceof Uint8Array) || ((status === 204n || status === 304n) && body.length > 0) || !__ef_http_header_value(contentType)) {
    __ef_http_status(res, 500, false);
    return;
  }
  const headers = { 'Content-Length': String(body.length) };
  if (contentType !== '') headers['Content-Type'] = contentType;
  res.writeHead(Number(status), headers);
  res.end(body);
};
// Publication runs inside this guard: a host rejection before the head is
// sent becomes an empty 500; after it, the response is aborted.
const __ef_http_guard = (res, publish) => {
  try {
    publish();
  } catch {
    try {
      if (res.headersSent) res.destroy();
      else __ef_http_status(res, 500, false);
    } catch {
      res.destroy();
    }
  }
};
// HTTP listen addresses and listener failures are Effra-owned text, the same
// bytes as runtime/effra/http.go (parseHTTPAddress, httpListenError). The
// result is the listen host and port, or the IoError message.
const __ef_http_address = address => {
  const invalid = reason => ({ error: 'invalid HTTP address ' + __ef_quoteText(address) + ': ' + reason });
  let host, port;
  if (address.startsWith('[')) {
    const end = address.indexOf(']');
    if (end < 0) return invalid('unbalanced brackets');
    host = address.slice(1, end);
    port = address.slice(end + 1);
    if (!port.startsWith(':')) return invalid('missing port');
    port = port.slice(1);
    if (host === '' || /[[\]]/.test(host) || !host.includes(':')) return invalid('brackets must enclose an IPv6 address');
  } else {
    const colon = address.lastIndexOf(':');
    if (colon < 0) return invalid('missing port');
    host = address.slice(0, colon);
    port = address.slice(colon + 1);
    if (/[[\]]/.test(host)) return invalid('unbalanced brackets');
    if (host.includes(':')) return invalid('an IPv6 host must be in brackets');
  }
  if (!/^[0-9]{1,5}$/.test(port) || Number(port) > 65535) return invalid('port must be a decimal number from 0 to 65535');
  return { host: host === '' ? undefined : host, port: Number(port) };
};
// __ef_http_parse_addr mirrors Go's netip.ParseAddr, the one place a listen
// host or a lookup answer is read as a numeric address: the zone is split
// off first, the address part is classified, and the zone is valid only when
// non-empty and on an IPv6 address. The result is { family, address, zone },
// or undefined for anything that is not a numeric address.
const __ef_http_parse_addr = (isIP, text) => {
  if (typeof text !== 'string') return undefined;
  const percent = text.indexOf('%');
  const address = percent < 0 ? text : text.slice(0, percent);
  const zone = percent < 0 ? '' : text.slice(percent + 1);
  const family = isIP(address);
  if (family === 0) return undefined;
  if (percent >= 0 && (family !== 6 || zone === '')) return undefined;
  return { family, address, zone };
};
// __ef_http_ipv4 is the dotted IPv4 form of a parsed address that Go's
// IP.To4 accepts: an IPv4 address or an IPv4-mapped IPv6 address
// (::ffff:a.b.c.d). Go listens on the 4-byte form of such an address and
// ignores the zone for the IPv4 socket. Anything else is undefined.
const __ef_http_ipv4 = parsed => {
  if (parsed.family === 4) return parsed.address;
  let canonical;
  try { canonical = new URL('http://[' + parsed.address + ']/').hostname; } catch { return undefined; }
  const mapped = /^\[::ffff:([0-9a-f]{1,4}):([0-9a-f]{1,4})\]$/.exec(canonical);
  if (mapped === null) return undefined;
  const high = parseInt(mapped[1], 16), low = parseInt(mapped[2], 16);
  return [high >> 8, high & 255, low >> 8, low & 255].join('.');
};
// __ef_http_bind_host is the host a parsed address binds: its IPv4 form when
// it has one, else the IPv6 address with its zone.
const __ef_http_bind_host = parsed => __ef_http_ipv4(parsed) ?? (parsed.zone === '' ? parsed.address : parsed.address + '%' + parsed.zone);
// __ef_http_select is the host a listener binds for a name's answers, as Go's
// net.Listen binds: the first answer IP.To4 accepts, in its IPv4 form, else
// the first numeric answer. Undefined when no answer is numeric.
const __ef_http_select = (isIP, answers) => {
  const numeric = answers.map(answer => __ef_http_parse_addr(isIP, answer?.address)).filter(parsed => parsed !== undefined);
  const selected = numeric.find(parsed => __ef_http_ipv4(parsed) !== undefined) ?? numeric[0];
  return selected === undefined ? undefined : __ef_http_bind_host(selected);
};
// __ef_http_listen_target is the listen host and port. A numeric host binds
// directly. A name is looked up exactly once, here, and the listener binds
// the selected numeric answer, so listen() never resolves again and binds
// what the lookup returned. Any lookup failure, or no numeric answer, is
// `host lookup failed`, because hosts report it differently (Node:
// getaddrinfo ENOTFOUND; Bun: listen ENOENT or EAGAIN).
const __ef_http_listen_target = (address, isIP, lookup) => Effect.gen(function* () {
  const parsed = __ef_http_address(address);
  if (parsed.error !== undefined) return yield* Effect.fail({ _tag: 'IoError', message: parsed.error });
  const { host, port } = parsed;
  if (host === undefined) return { host, port };
  const literal = __ef_http_parse_addr(isIP, host);
  if (literal !== undefined) return { host: __ef_http_bind_host(literal), port };
  const answers = yield* Effect.promise(() => Promise.resolve().then(() => lookup(host, { all: true })).then(found => Array.isArray(found) ? found : [], () => []));
  const selected = __ef_http_select(isIP, answers);
  if (selected === undefined) return yield* Effect.fail({ _tag: 'IoError', message: 'HTTP listen on ' + __ef_quoteText(address) + ': host lookup failed' });
  return { host: selected, port };
});
const __ef_http_listen_error = (address, error) => {
  const prefix = 'HTTP listen on ' + __ef_quoteText(address) + ': ';
  switch (error?.code) {
    case 'EADDRINUSE': return prefix + 'address in use';
    case 'EADDRNOTAVAIL': return prefix + 'address not available';
    case 'EACCES': case 'EPERM': return prefix + 'permission denied';
  }
  return prefix + String(error?.message ?? error);
};
const __ef_http_bound = server => {
  const bound = server.address();
  const host = bound.family === 'IPv6' || bound.family === 6 ? '[' + bound.address + ']' : bound.address;
  return host + ':' + bound.port;
};

// __ef_http_serve owns one listener. onRequest(req, res, transport) handles one
// request. transport.exchange(res, release) owns one response until it has
// been handed to the OS or its connection closed, then runs release once.
// transport.run executes an Effra request program in its own owned scope with
// the listener's services and, once that scope has closed, publishes its exit
// through respond unless the client is gone.
const __ef_http_serve = (address, timeouts, onRequest) => Effect.gen(function* () {
  const [{ createServer }, { isIP }, { lookup }] = yield* Effect.promise(() => Promise.all([__ef_host_node_http(), __ef_host_node_net(), __ef_host_node_dns_promises()]));
  const { host, port } = yield* __ef_http_listen_target(address, isIP, lookup);
  const context = yield* Effect.context();
  const transport = { closing: false, fibers: new Map(), exchanges: new Set(), reading: new WeakMap() };
  transport.exchange = (res, release = () => {}) => {
    let settle;
    const exchange = { res, done: new Promise(resolve => { settle = resolve; }) };
    exchange.retire = () => {
      if (!transport.exchanges.delete(exchange)) return;
      release();
      settle();
    };
    transport.exchanges.add(exchange);
    res.once('finish', exchange.retire);
    res.once('close', exchange.retire);
    return exchange;
  };
  transport.run = (res, program, respond) => {
    const fiber = Effect.runForkWith(context)(__ef_scoped(program));
    const disconnect = () => { if (!res.writableFinished) fiber.interruptUnsafe(); };
    res.once('close', disconnect);
    let completed = false;
    const joined = new Promise(resolve => fiber.addObserver(exit => {
      completed = true;
      transport.fibers.delete(fiber);
      res.off('close', disconnect);
      // The client is gone: no response remains possible.
      if (res.destroyed || res.socket?.destroyed) res.destroy();
      else __ef_http_guard(res, () => respond(exit));
      resolve();
    }));
    if (!completed) transport.fibers.set(fiber, joined);
    if (transport.closing) fiber.interruptUnsafe();
  };
  // Header timeouts are checked at this interval rather than node's 30s default.
  const server = createServer({ connectionsCheckingInterval: Math.min(timeouts.readHeaderMillis, 1000) }, (req, res) => onRequest(req, res, transport));
  // Parser failures belong to the transport. The request being read stops
  // reading and keeps its exchange, and so its admission, until the error
  // response has been handed to the OS or the connection closed (the end
  // callback runs either way); its own response is never published, so only
  // this write retires it. The transport then destroys the connection itself,
  // as node:http's default parser-error path does: ending only half-closes
  // it, and a client that keeps its side open would otherwise hold shutdown
  // open on hosts whose closeAllConnections leaves such a socket alone (Bun).
  // The error response goes straight to the socket, outside node:http's
  // response queue: it follows only bytes already written there, and an
  // earlier pipelined response not yet written is never sent (a known host
  // limit, docs/runtime.md). A header timeout closes silently.
  server.on('clientError', (error, socket) => {
    const reading = transport.reading.get(socket);
    reading?.stop();
    if (error?.code === 'ERR_HTTP_REQUEST_TIMEOUT' || !socket.writable) socket.destroy();
    else socket.end('HTTP/1.1 ' + (error?.code === 'HPE_HEADER_OVERFLOW' ? '431 Request Header Fields Too Large' : '400 Bad Request') + '\r\nContent-Length: 0\r\nConnection: close\r\n\r\n', () => {
      reading?.exchange.retire();
      socket.destroy();
    });
  });
  server.headersTimeout = timeouts.readHeaderMillis;
  server.requestTimeout = 0;
  server.keepAliveTimeout = timeouts.idleMillis;
  const listen = Effect.callback(resume => {
    const failed = error => resume(Effect.fail({ _tag: 'IoError', message: __ef_http_listen_error(address, error) }));
    server.once('error', failed);
    server.listen(port, host, () => { server.off('error', failed); resume(Effect.void); });
  });
  // Shutdown stops admission (later requests receive 503), cancels and joins
  // every request scope (each publishes or abandons its response), then gives
  // the remaining transport I/O idleMillis to complete before aborting its
  // connections, and finally closes the listener and every connection. The
  // listener closes last because node:http's close() also destroys
  // connections whose response is still being written.
  const shutdown = Effect.promise(async () => {
    transport.closing = true;
    while (transport.fibers.size > 0) {
      for (const fiber of transport.fibers.keys()) fiber.interruptUnsafe();
      await Promise.all(transport.fibers.values());
    }
    const abort = setTimeout(() => { for (const exchange of transport.exchanges) exchange.res.destroy(); }, timeouts.idleMillis);
    await Promise.all([...transport.exchanges].map(exchange => exchange.done));
    clearTimeout(abort);
    const closed = new Promise(resolve => server.close(() => resolve()));
    server.closeAllConnections?.();
    await closed;
  });
  return yield* Effect.acquireUseRelease(listen, () => Effect.gen(function* () {
    console.log('listening http://' + __ef_http_bound(server));
    return yield* Effect.never;
  }), () => shutdown);
});

const __ef_http_listen = (address, source, handler) => Effect.suspend(() => {
  let limits;
  try { limits = __ef_http_limits(source); } catch (error) { return Effect.die(error); }
  let admitted = 0;
  return __ef_http_serve(address, limits, (req, res, transport) => {
    if (transport.closing || admitted >= limits.maxActive) { __ef_http_status(res, 503, true); return; }
    const declared = req.headers['content-length'];
    if (declared !== undefined && Number(declared) > limits.maxBodyBytes) { __ef_http_status(res, 413, true); return; }
    // Admission covers the body read and the response until it has been
    // handed to the OS or its connection closed, so the buffered bodies of
    // both directions stay within maxActive.
    admitted++;
    const exchange = transport.exchange(res, () => { admitted--; });
    const reading = { done: false, chunks: [], size: 0 };
    const finish = () => { reading.done = true; clearTimeout(timer); transport.reading.delete(req.socket); };
    const abort = () => { if (reading.done) return; finish(); req.socket?.destroy(); };
    const timer = setTimeout(abort, limits.readBodyMillis);
    transport.reading.set(req.socket, { stop: finish, exchange });
    req.on('data', chunk => {
      if (reading.done) return;
      reading.size += chunk.length;
      if (reading.size > limits.maxBodyBytes) { finish(); __ef_http_status(res, 413, true); req.resume(); return; }
      reading.chunks.push(chunk);
    });
    req.on('error', () => { if (reading.done) return; finish(); __ef_http_status(res, 400, true); });
    req.on('close', abort);
    req.on('end', () => {
      if (reading.done) return;
      finish();
      if (transport.closing) { __ef_http_status(res, 503, true); return; }
      const request = { method: req.method, path: __ef_http_path(req.url), contentType: req.headers['content-type'] ?? '', body: new Uint8Array(Buffer.concat(reading.chunks)) };
      transport.run(res, handler(request), exit => {
        if (Exit.isSuccess(exit)) __ef_http_publish(res, __ef_http_reply(exit.value));
        else if (transport.closing && Cause.hasInterrupts(exit.cause)) __ef_http_status(res, 503, true);
        else __ef_http_status(res, 500, transport.closing);
      });
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
  listen: (address, limits, handler) => __ef_http_listen(address, limits, handler),
  text: text => Effect.sync(() => new TextEncoder().encode(text)),
};
