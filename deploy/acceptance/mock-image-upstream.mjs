import http from 'node:http'
import { randomUUID } from 'node:crypto'

// Local acceptance fixture only. It deliberately returns PNG even when JPEG
// is requested, so downloaded content must be classified from its bytes.
const image = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aO7sAAAAASUVORK5CYII=', 'base64')
const requests = []
let active = 0
let maxActive = 0

function json(response, status, body) {
  response.writeHead(status, { 'content-type': 'application/json' })
  response.end(JSON.stringify(body))
}

function multipartFields(raw, contentType) {
  const boundary = /boundary=(?:"([^"]+)"|([^;]+))/i.exec(contentType)
  if (!boundary) throw new Error('missing multipart boundary')
  const fields = {}
  const references = []
  for (const part of raw.toString('latin1').split(`--${boundary[1] || boundary[2]}`)) {
    const separator = part.indexOf('\r\n\r\n')
    if (separator < 0) continue
    const headers = part.slice(0, separator)
    const name = /name="([^"]+)"/i.exec(headers)?.[1]
    const value = part.slice(separator + 4).replace(/\r\n$/, '')
    if (!name) continue
    if (/filename="/i.test(headers)) {
      references.push({ name, bytes: Buffer.byteLength(value, 'latin1'), png: Buffer.from(value, 'latin1').subarray(0, 8).equals(image.subarray(0, 8)) })
    } else fields[name] = value
  }
  return { fields, references }
}

const server = http.createServer(async (request, response) => {
  const path = new URL(request.url || '/', 'http://fixture.local').pathname
  if (request.method === 'GET' && path === '/health') return json(response, 200, { status: 'ok' })
  if (request.method === 'GET' && path === '/__smoke/stats') return json(response, 200, { requests, max_active: maxActive })
  if (request.method === 'GET' && path === '/v1/models') {
    return json(response, 200, { object: 'list', data: [{ id: 'gpt-image-2', object: 'model', owned_by: 'local-image-fixture' }] })
  }
  if (request.method !== 'POST' || !['/v1/images/generations', '/v1/images/edits'].includes(path)) {
    return json(response, 404, { error: { message: 'fixture route not found' } })
  }
  try {
    const chunks = []
    let size = 0
    for await (const chunk of request) {
      size += chunk.length
      if (size > 2 * 1024 * 1024) throw new Error('fixture body too large')
      chunks.push(chunk)
    }
    const raw = Buffer.concat(chunks)
    const contentType = request.headers['content-type'] || ''
    const parsed = contentType.startsWith('multipart/form-data')
      ? multipartFields(raw, contentType)
      : { fields: JSON.parse(raw.toString('utf8')), references: [] }
    const fields = parsed.fields
    if (fields.model !== 'gpt-image-2') throw new Error(`unexpected model ${fields.model}`)
    if (fields.n !== undefined && Number(fields.n) !== 1) throw new Error('each upstream request must generate only one image')
    if (path.endsWith('/edits') && (!parsed.references.length || parsed.references.some(ref => !ref.png))) {
      throw new Error('edits must contain intact PNG reference bytes')
    }
    requests.push({
      path,
      model: fields.model,
      prompt: fields.prompt,
      n: fields.n === undefined ? null : Number(fields.n),
      size: fields.size,
      references: parsed.references,
      idempotency_key: request.headers['idempotency-key'] || null,
    })
    active++
    maxActive = Math.max(maxActive, active)
    await new Promise(resolve => setTimeout(resolve, 150))
    active--
    response.setHeader('X-Request-Id', randomUUID())
    return json(response, 200, {
      created: Math.floor(Date.now() / 1000),
      data: [{ b64_json: image.toString('base64'), revised_prompt: fields.prompt }],
      usage: { input_tokens: 5, output_tokens: 5, total_tokens: 10, input_tokens_details: { text_tokens: 5, image_tokens: 0 } },
    })
  } catch (error) {
    return json(response, 400, { error: { message: error.message, type: 'fixture_contract_error' } })
  }
})

server.listen(8080, '0.0.0.0')
