import http from 'node:http'

// Synthetic fixture only; never forwards a request to a real model provider.
const requests = []
const json = (res, status, body, headers = {}) => {
  res.writeHead(status, { 'content-type': 'application/json', ...headers })
  res.end(JSON.stringify(body))
}
http.createServer(async (req, res) => {
  if (req.url === '/health') return json(res, 200, { status: 'ok' })
  if (req.url === '/stats') return json(res, 200, requests)
  let raw = ''
  for await (const chunk of req) raw += chunk
  let body
  try { body = JSON.parse(raw) } catch { return json(res, 400, { error: { code: 'invalid_json' } }) }
  // Account creation performs a separate capability probe. Acknowledge it,
  // without counting it as an attempted user request or injecting a fault.
  if (body.tools?.some(tool => tool.name === 'probe_ping')) {
    return json(res, 200, { id: 'resp_probe', object: 'response', status: 'completed', model: body.model,
      output: [{ type: 'function_call', name: 'probe_ping', call_id: 'call_probe', arguments: '{"ok":true}', status: 'completed' }] })
  }
  const key = (req.headers.authorization || '').replace('Bearer ', '')
  const [scenario, slot] = key.split(':')
  requests.push({ scenario, slot, path: req.url, stream: Boolean(body.stream), model: body.model })
  const emit = value => res.write(`data: ${JSON.stringify(value)}\n\n`)
  if (scenario === 'long_connection') {
    if (!body.stream) return json(res, 400, { error: { code: 'stream_required' } })
    res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache', 'x-accel-buffering': 'no' })
    const responseId = `resp_long_${crypto.randomUUID()}`
    emit({ type: 'response.created', response: { id: responseId, object: 'response', status: 'in_progress', model: body.model } })
    emit({ type: 'response.output_text.delta', response_id: responseId, output_index: 0, content_index: 0, delta: 'long-stream-ready' })
    const heartbeat = setInterval(() => { if (!res.destroyed) res.write(': heartbeat\n\n') }, 20_000)
    await new Promise(resolve => setTimeout(resolve, 660_000))
    clearInterval(heartbeat)
    if (res.destroyed) return
    emit({ type: 'response.completed', response: { id: responseId, object: 'response', status: 'completed', model: body.model,
      output: [{ id: 'msg_long_fixture', type: 'message', role: 'assistant', status: 'completed', content: [{ type: 'output_text', text: 'long-stream-ready', annotations: [] }] }],
      usage: { input_tokens: 10, output_tokens: 2, total_tokens: 12 } } })
    return res.end()
  }
  if (scenario?.startsWith('slow_')) {
    let heartbeat
    if (body.stream && scenario !== 'slow_headers') {
      res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' })
      emit({ type: 'response.created', response: { id: 'resp_slow', status: 'in_progress', ...(scenario === 'slow_usage' ? { usage: { input_tokens: 10, output_tokens: 0 } } : {}) } })
      if (scenario === 'slow_content') emit({ type: 'response.output_text.delta', delta: 'already-output' })
      heartbeat = setInterval(() => { if (!res.destroyed) res.write(': ping\n\n') }, 1000)
    }
    await new Promise(resolve => setTimeout(resolve, 7000))
    clearInterval(heartbeat)
    if (res.destroyed) return
  }
  if (slot === 'primary') {
    const errors = {
      overload: [503, 'server_is_overloaded'], quota: [429, 'insufficient_quota'],
      model: [404, 'model_not_found'], server: [502, 'server_error'],
      timeout: [524, 'server_error'], auth: [401, 'invalid_api_key'],
      rate: [429, 'rate_limit_exceeded'], safety: [403, 'content_policy_violation'],
      parameter: [502, 'invalid_parameter']
    }
    if (errors[scenario]) {
      const [status, code] = errors[scenario]
      return json(res, status, { error: { code, type: 'fixture_error', message: 'synthetic upstream failure' } }, scenario === 'rate' ? { 'retry-after': '30' } : {})
    }
    if (scenario === 'stream' || scenario === 'partial') {
      res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' })
      if (scenario === 'partial') {
        emit({ type: 'response.output_text.delta', response_id: 'resp_fixture', output_index: 0, content_index: 0, delta: 'already-output' })
        await new Promise(resolve => setTimeout(resolve, 80))
      }
      emit({ type: 'response.failed', response: { id: 'resp_fixture', status: 'failed', error: { code: 'server_error', type: 'server_error', status_code: 503, message: 'synthetic upstream failure' } } })
      return res.end()
    }
  }
  // CN-compatible groups translate Responses into native Chat Completions.
  // Reply in the requested wire protocol instead of returning a Responses body
  // that a real Chat provider would never send.
  if (req.url === '/v1/chat/completions') {
    const id = `chatcmpl_${crypto.randomUUID()}`
    const usage = { prompt_tokens: 10, completion_tokens: 2, total_tokens: 12 }
    if (!body.stream) return json(res, 200, {
      id, object: 'chat.completion', model: body.model,
      choices: [{ index: 0, message: { role: 'assistant', content: 'backup-ok' }, finish_reason: 'stop' }], usage
    })
    res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' })
    emit({ id, object: 'chat.completion.chunk', model: body.model, choices: [{ index: 0, delta: { role: 'assistant', content: 'backup-ok' }, finish_reason: null }] })
    emit({ id, object: 'chat.completion.chunk', model: body.model, choices: [{ index: 0, delta: {}, finish_reason: 'stop' }], usage })
    res.write('data: [DONE]\n\n')
    return res.end()
  }
  const value = {
    id: `resp_${crypto.randomUUID()}`, object: 'response', status: 'completed', model: body.model,
    output: [{ id: 'msg_fixture', type: 'message', role: 'assistant', status: 'completed', content: [{ type: 'output_text', text: 'backup-ok', annotations: [] }] }],
    usage: { input_tokens: 10, output_tokens: 2, total_tokens: 12 }
  }
  if (!body.stream) return json(res, 200, value)
  if (!res.headersSent) res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' })
  emit({ type: 'response.output_text.delta', response_id: value.id, output_index: 0, content_index: 0, delta: 'backup-ok' })
  emit({ type: 'response.completed', response: value })
  res.end()
}).listen(8080, '0.0.0.0')
