// Synthetic, deliberately non-deduplicating upstream for image-studio-recovery.ps1.
// Never use a real upstream: receipt and response release are separate checkpoints.
import http from 'node:http';
import { randomUUID } from 'node:crypto';

const token = process.env.RECOVERY_CONTROL_TOKEN;
const run = process.env.RECOVERY_RUN;
if (!/^[a-f0-9]{32}$/.test(token || '') || !/^[a-f0-9]{10}$/.test(run || '')) {
  throw new Error('Synthetic recovery token and run ID are required');
}
const png = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aO7sAAAAASUVORK5CYII=';
const requests = [];
const pending = new Map();
const json = (res, status, body) => {
  res.writeHead(status, { 'content-type': 'application/json' });
  res.end(JSON.stringify(body));
};
async function body(req) {
  const chunks = [];
  let length = 0;
  for await (const chunk of req) {
    length += chunk.length;
    if (length > 16384) throw new Error('Fixture request is too large');
    chunks.push(chunk);
  }
  return JSON.parse(Buffer.concat(chunks).toString('utf8'));
}

http.createServer(async (req, res) => {
  try {
    if (req.method === 'GET' && req.url === '/health') return json(res, 200, { status: 'ok', run });
    if (req.url?.startsWith('/__recovery/')) {
      if (req.headers['x-recovery-control'] !== token) return json(res, 403, { error: 'fixture control token required' });
      if (req.method === 'GET' && req.url === '/__recovery/stats') {
        return json(res, 200, { run, requests, pending: [...pending.keys()] });
      }
      if (req.method === 'POST' && req.url === '/__recovery/release') {
        const input = await body(req);
        // The unknown case must never acquire a fabricated result after restart.
        if (input.case !== 'durable') return json(res, 400, { error: 'Only the durable case may be released' });
        const held = pending.get('durable');
        if (!held || held.res.destroyed) return json(res, 409, { error: 'No live durable request to release' });
        held.record.response_sent_at = new Date().toISOString();
        held.res.setHeader('X-Request-Id', held.record.request_id);
        json(held.res, 200, {
          created: Math.floor(Date.now() / 1000), data: [{ b64_json: png }],
          usage: { input_tokens: 5, output_tokens: 5, total_tokens: 10 },
        });
        pending.delete('durable');
        return json(res, 200, { released: 'durable' });
      }
      return json(res, 404, { error: 'Unknown fixture control endpoint' });
    }
    if (req.method === 'GET' && req.url === '/v1/models') {
      return json(res, 200, { object: 'list', data: [{ id: 'gpt-image-2', object: 'model' }] });
    }
    if (req.method !== 'POST' || req.url !== '/v1/images/generations') return json(res, 404, { error: 'Unexpected image endpoint' });
    const input = await body(req);
    const kind = ['durable', 'unknown'].find(value => input.prompt === `recovery:${run}:${value}`);
    const record = {
      case: kind || 'invalid', request_id: randomUUID(), received_at: new Date().toISOString(),
      model: input.model, n: input.n, size: input.size,
      idempotency_key: req.headers['idempotency-key'] || '', response_sent_at: null, disconnected_at: null,
    };
    // Record every attempt, including retries; do not hide double dispatch behind deduplication.
    requests.push(record);
    if (!kind || input.model !== 'gpt-image-2' || input.n !== 1 || input.size !== '2048x2048' ||
        !/^sub2api-batch-[a-f0-9]{64}$/.test(record.idempotency_key) ||
        req.headers.authorization !== 'Bearer local-recovery-fixture-not-a-real-key') {
      return json(res, 400, { error: 'Synthetic managed_fanout contract mismatch' });
    }
    if (requests.filter(item => item.case === kind).length !== 1) {
      return json(res, 409, { error: 'Duplicate upstream dispatch detected by fixture' });
    }
    pending.set(kind, { res, record });
    res.on('close', () => {
      if (!res.writableEnded) record.disconnected_at = new Date().toISOString();
      if (pending.get(kind)?.res === res) pending.delete(kind);
    });
    // No timer and no automatic acknowledgement. The harness releases durable;
    // unknown stays in flight until the real app process is killed.
  } catch (error) {
    if (!res.headersSent) json(res, 400, { error: String(error.message) });
    else res.destroy();
  }
}).listen(8080, '0.0.0.0'); // Container-only; harness publishes 127.0.0.1 exclusively.
