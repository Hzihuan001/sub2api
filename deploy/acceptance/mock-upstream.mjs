import http from 'node:http'

const models = ['gpt-5.6-sol', 'deepseek-flash', 'deepseek-v4-pro']

function sendJSON(response, status, body) {
  response.writeHead(status, { 'content-type': 'application/json' })
  response.end(JSON.stringify(body))
}

function completion(model, content = 'acceptance-ok') {
  return {
    id: `chatcmpl-${crypto.randomUUID()}`,
    object: 'chat.completion',
    created: Math.floor(Date.now() / 1000),
    model,
    choices: [{ index: 0, message: { role: 'assistant', content }, finish_reason: 'stop' }],
    usage: { prompt_tokens: 12, completion_tokens: 5, total_tokens: 17 }
  }
}

function responseBody(model) {
  return {
    id: `resp_${crypto.randomUUID()}`,
    object: 'response',
    created_at: Math.floor(Date.now() / 1000),
    status: 'completed',
    model,
    output: [{ id: `msg_${crypto.randomUUID()}`, type: 'message', status: 'completed', role: 'assistant', content: [{ type: 'output_text', text: 'acceptance-ok', annotations: [] }] }],
    usage: { input_tokens: 12, output_tokens: 5, total_tokens: 17, input_tokens_details: { cached_tokens: 2 } }
  }
}

const server = http.createServer((request, response) => {
  if (request.method === 'GET' && (request.url === '/health' || request.url === '/')) {
    return sendJSON(response, 200, { status: 'ok' })
  }
  if (request.method === 'GET' && request.url?.endsWith('/models')) {
    return sendJSON(response, 200, { object: 'list', data: models.map(id => ({ id, object: 'model', owned_by: 'moshu-acceptance' })) })
  }

  let raw = ''
  request.on('data', chunk => { raw += chunk })
  request.on('end', () => {
    let body = {}
    try { body = raw ? JSON.parse(raw) : {} } catch { return sendJSON(response, 400, { error: { message: 'invalid json' } }) }
    const model = typeof body.model === 'string' ? body.model : 'gpt-5.6-sol'
    if (!models.includes(model)) return sendJSON(response, 400, { error: { message: `unsupported model ${model}` } })

    if (request.url?.endsWith('/chat/completions')) {
      if (!body.stream) return sendJSON(response, 200, completion(model))
      response.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache', connection: 'keep-alive' })
      response.write(`data: ${JSON.stringify({ id: `chatcmpl-${crypto.randomUUID()}`, object: 'chat.completion.chunk', created: Math.floor(Date.now() / 1000), model, choices: [{ index: 0, delta: { role: 'assistant', content: 'acceptance-' }, finish_reason: null }] })}\n\n`)
      response.write(`data: ${JSON.stringify({ id: `chatcmpl-${crypto.randomUUID()}`, object: 'chat.completion.chunk', created: Math.floor(Date.now() / 1000), model, choices: [{ index: 0, delta: { content: 'ok' }, finish_reason: 'stop' }], usage: { prompt_tokens: 12, completion_tokens: 5, total_tokens: 17 } })}\n\n`)
      response.end('data: [DONE]\n\n')
      return
    }

    if (request.url?.endsWith('/responses')) {
      const value = responseBody(model)
      if (!body.stream) return sendJSON(response, 200, value)
      response.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache', connection: 'keep-alive' })
      response.write(`event: response.output_text.delta\ndata: ${JSON.stringify({ type: 'response.output_text.delta', delta: 'acceptance-ok' })}\n\n`)
      response.write(`event: response.completed\ndata: ${JSON.stringify({ type: 'response.completed', response: value })}\n\n`)
      response.end()
      return
    }

    sendJSON(response, 404, { error: { message: 'not found' } })
  })
})

server.listen(8080, '0.0.0.0')
