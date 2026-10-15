import http from 'node:http'
import { performance } from 'node:perf_hooks'

// Local synthetic fixture: no provider/network forwarding and no paid API.
const pause = ms => new Promise(resolve => setTimeout(resolve, ms))
const json = (response, value) => {
  response.writeHead(200, { 'content-type': 'application/json' })
  response.end(JSON.stringify(value))
}

if (process.argv[2] === 'server') {
  const counters = { total: 0, http: 0, sse: 0, probe: 0, rejected: 0 }
  http.createServer(async (request, response) => {
    if (request.url === '/health') return json(response, { status: 'ok' })
    if (request.url === '/stats') return json(response, counters)
    counters.total++
    let raw = ''
    for await (const chunk of request) raw += chunk
    let body
    try { body = JSON.parse(raw) } catch { counters.rejected++; response.writeHead(400); return response.end() }
    if (body.tools?.some(tool => tool.name === 'probe_ping')) {
      counters.probe++
      return json(response, { id: 'resp_probe', object: 'response', status: 'completed', model: body.model,
        output: [{ type: 'function_call', name: 'probe_ping', call_id: 'call_probe', arguments: '{"ok":true}', status: 'completed' }] })
    }
    if (request.url !== '/v1/responses' || body.model !== 'gpt-4o-mini' || body.input !== 'modular-performance-local-fixture') {
      counters.rejected++
      response.writeHead(400)
      return response.end('unexpected synthetic request')
    }
    counters[body.stream ? 'sse' : 'http']++
    const value = {
      id: `resp_${crypto.randomUUID()}`, object: 'response', status: 'completed', model: body.model,
      output: [{ id: 'msg_perf', type: 'message', role: 'assistant', status: 'completed', content: [{ type: 'output_text', text: 'synthetic-perf-ok', annotations: [] }] }],
      usage: { input_tokens: 10, output_tokens: 2, total_tokens: 12 }
    }
    await pause(5)
    if (!body.stream) return json(response, value)
    response.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' })
    response.write(`data: ${JSON.stringify({ type: 'response.output_text.delta', response_id: value.id, output_index: 0, content_index: 0, delta: 'synthetic-perf-ok' })}\n\n`)
    await pause(5)
    response.write(`data: ${JSON.stringify({ type: 'response.completed', response: value })}\n\n`)
    response.end()
  }).listen(8080, '0.0.0.0')
} else if (process.argv[2] === 'client') {
  let stdin = ''
  for await (const chunk of process.stdin) stdin += chunk
  const config = JSON.parse(stdin)
  // Hard-coded service name: never accept a production destination/key URL.
  if (![true, false].includes(config.stream) || !Number.isInteger(config.count) || config.count < 1 || config.count > 200 || config.concurrency !== 2) {
    throw new Error('invalid bounded local workload')
  }
  const rows = []
  let next = 0
  await Promise.all(Array.from({ length: config.concurrency }, async () => {
    while (next < config.count) {
      const index = next++
      const started = performance.now()
      const response = await fetch('http://sub2api:8080/v1/responses', {
        method: 'POST', headers: { authorization: `Bearer ${config.key}`, 'content-type': 'application/json' },
        body: JSON.stringify({ model: 'gpt-4o-mini', input: 'modular-performance-local-fixture', stream: config.stream }),
        signal: AbortSignal.timeout(15000)
      })
      const headersMs = performance.now() - started
      let firstBodyMs = null
      let text = ''
      for await (const bytes of response.body) {
        firstBodyMs ??= performance.now() - started
        text += Buffer.from(bytes).toString('utf8')
      }
      const totalMs = performance.now() - started
      if (response.status !== 200 || !text.includes('synthetic-perf-ok') || (config.stream && !text.includes('response.completed'))) {
        throw new Error(`synthetic request ${index} failed status=${response.status} body=${text.slice(0, 300)}`)
      }
      rows.push({ index, headersMs, firstBodyMs, totalMs })
    }
  }))
  const percentile = (field, p) => rows.map(row => row[field]).sort((a, b) => a - b)[Math.ceil(rows.length * p) - 1]
  console.log(JSON.stringify({ count: rows.length, concurrency: config.concurrency, stream: config.stream,
    p50TtfbMs: percentile('firstBodyMs', .5), p95TtfbMs: percentile('firstBodyMs', .95),
    p50TotalMs: percentile('totalMs', .5), p95TotalMs: percentile('totalMs', .95), rows }))
} else {
  throw new Error('select server or client')
}
