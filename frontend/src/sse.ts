// POST SSE parser for the guided chat stream. Reads a fetch() response body
// through a ReadableStream, buffers fragmented frames, decodes multi-byte
// UTF-8 safely and validates the event contract strictly:
//   meta (once, first, with skill/citations) -> delta* -> done | error (once, last)
// Uses only erasable TypeScript so `node --test` can execute the tests
// directly with type stripping.

export interface CitationPayload {
  ordinal: number;
  material_id: string;
  material_title: string;
  chunk_id: number;
  chunk_index: number;
  char_start: number;
  char_end: number;
  excerpt: string;
}

export type StreamEvent =
  | {
      type: 'meta';
      status: 'ok';
      skill: { id: number; name: string } | null;
      citations: CitationPayload[];
      conversation_id: string;
      user_message_id: string;
      assistant_message_id: string;
    }
  | { type: 'delta'; text: string }
  | { type: 'done'; assistant_message_id: string; status: string; citations: CitationPayload[] }
  // retryable: tri-state — null when the server omitted the flag.
  | { type: 'error'; code: string; message: string; retryable: boolean | null };

// StreamError carries an HTTP status (0 = protocol/transport level) so the UI
// can branch on 401 vs retryable failures without parsing message text.
export class StreamError extends Error {
  status: number;
  code: string;
  retryable: boolean;
  constructor(status: number, code: string, retryable: boolean) {
    super(code);
    this.name = 'StreamError';
    this.status = status;
    this.code = code;
    this.retryable = retryable;
  }
}

export type StreamTerminal =
  | { status: 'completed'; assistantMessageId: string; citations: CitationPayload[] }
  | { status: 'error'; code: string; message: string; retryable: boolean };

export interface StreamOptions {
  conversationId: string;
  question: string;
  token: string;
  signal?: AbortSignal;
  // Test hook: overrides the request path origin. Keep undefined in prod.
  baseUrl?: string;
  onEvent: (e: StreamEvent) => void;
}

// Codes that must keep identity/history and offer a retry (spec 7.8).
const RETRYABLE_CODES = new Set([
  'retrieval_unavailable',
  'model_unavailable',
  'model_timeout',
  'output_limit_exceeded',
  'internal',
  'service_unavailable',
]);

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

function asString(v: unknown): string | null {
  return typeof v === 'string' ? v : null;
}

function asNonEmptyString(v: unknown): string | null {
  return typeof v === 'string' && v.length > 0 ? v : null;
}

function asCitation(v: unknown): CitationPayload | null {
  if (!isRecord(v)) return null;
  const ordinal = v['ordinal'];
  const materialId = asNonEmptyString(v['material_id']);
  if (typeof ordinal !== 'number' || !Number.isInteger(ordinal) || ordinal < 1) return null;
  if (!materialId) return null;
  return {
    ordinal,
    material_id: materialId,
    material_title: asString(v['material_title']) ?? '',
    chunk_id: typeof v['chunk_id'] === 'number' ? v['chunk_id'] : 0,
    chunk_index: typeof v['chunk_index'] === 'number' ? v['chunk_index'] : 0,
    char_start: typeof v['char_start'] === 'number' ? v['char_start'] : 0,
    char_end: typeof v['char_end'] === 'number' ? v['char_end'] : 0,
    excerpt: asString(v['excerpt']) ?? '',
  };
}

// parseEvent validates one named SSE frame's JSON payload against the wire
// contract. Returns null when the payload is structurally invalid.
function parseEvent(name: string, data: string): StreamEvent | null {
  let payload: unknown;
  try {
    payload = JSON.parse(data);
  } catch {
    return null;
  }
  if (!isRecord(payload)) return null;
  switch (name) {
    case 'meta': {
      const conversationId=asNonEmptyString(payload['conversation_id']);
      const userMessageId=asNonEmptyString(payload['user_message_id']);
      const assistantMessageId=asNonEmptyString(payload['assistant_message_id']);
      if (!conversationId || !userMessageId || !assistantMessageId || payload['status'] !== 'ok' || !Array.isArray(payload['citations'])) return null;
      const citations: CitationPayload[]=[];
      for (const raw of payload['citations']) {const parsed=asCitation(raw); if(!parsed || parsed.ordinal!==citations.length+1) return null; citations.push(parsed);}
      let skill: {id:number;name:string}|null=null;
      if(payload['skill']!==null) {const raw=payload['skill']; if(!isRecord(raw)||typeof raw['id']!=='number'||!Number.isInteger(raw['id'])||raw['id']<1||!asNonEmptyString(raw['name']))return null;skill={id:raw['id'],name:raw['name'] as string};}
      return {type:'meta',status:'ok',skill,citations,conversation_id:conversationId,user_message_id:userMessageId,assistant_message_id:assistantMessageId};
    }
    case 'delta': {
      const text = asNonEmptyString(payload['text']);
      if (text === null) return null;
      return { type: 'delta', text };
    }
    case 'done': {
      const assistantMessageId = asNonEmptyString(payload['assistant_message_id']);
      const status = asString(payload['status']);
      if (!assistantMessageId || status !== 'completed') return null;
      if (!Array.isArray(payload['citations'])) return null;
      const citations: CitationPayload[] = [];
      for (const c of payload['citations']) {
        const parsed = asCitation(c);
        if (!parsed) return null;
        citations.push(parsed);
      }
      return { type: 'done', assistant_message_id: assistantMessageId, status, citations };
    }
    case 'error': {
      const code = asNonEmptyString(payload['code']);
      if (!code) return null;
      const flag = payload['retryable'];
      return {
        type: 'error',
        code,
        message: asString(payload['message']) ?? code,
        retryable: typeof flag === 'boolean' ? flag : null,
      };
    }
    default:
      return null;
  }
}

// Terminal state machine for event ordering. Throws StreamError('protocol_error')
// on any contract violation: duplicate start, events before start, events
// after a terminal, or a second terminal.
function makeOrderGuard() {
  let started = false;
  let terminal = false;
  let assistantId='';
  let citations: CitationPayload[]=[];
  return (e: StreamEvent): void => {
    if (terminal) throw new StreamError(0, 'protocol_error', false);
    switch (e.type) {
      case 'meta':
        if (started) throw new StreamError(0, 'protocol_error', false);
        started = true;
        assistantId=e.assistant_message_id;citations=e.citations;
        return;
      case 'delta':
        if (!started) throw new StreamError(0, 'protocol_error', false);
        return;
      case 'done':
        if(e.assistant_message_id!==assistantId || JSON.stringify(e.citations)!==JSON.stringify(citations))throw new StreamError(0,'protocol_error',false);
      case 'error':
        if (!started) throw new StreamError(0, 'protocol_error', false);
        terminal = true;
        return;
    }
  };
}

// splitFrames drains complete "\n\n"-separated frames from the buffer and
// returns the remainder. Handles \r\n line endings defensively.
function splitFrames(buffer: string): { frames: string[]; rest: string } {
  const frames: string[] = [];
  let start = 0;
  let idx = buffer.indexOf('\n\n');
  while (idx >= 0) {
    frames.push(buffer.slice(start, idx));
    start = idx + 2;
    idx = buffer.indexOf('\n\n', start);
  }
  return { frames, rest: buffer.slice(start) };
}

// parseFrame turns one raw frame block into an event name + data pair.
// Multiple data lines, comments-only frames and unknown fields follow SSE
// rules; a frame without an event name or with more than one data line is
// malformed under this server's single-line JSON contract.
function parseFrame(frame: string): { name: string; data: string } | null {
  let name: string | null = null;
  let data: string | null = null;
  for (const rawLine of frame.split('\n')) {
    const line = rawLine.endsWith('\r') ? rawLine.slice(0, -1) : rawLine;
    if (line === '' || line.startsWith(':')) continue;
    const colon = line.indexOf(':');
    if (colon < 0) return null;
    const field = line.slice(0, colon);
    let value = line.slice(colon + 1);
    if (value.startsWith(' ')) value = value.slice(1);
    if (field === 'event') {
      if (name !== null) return null;
      name = value;
    } else if (field === 'data') {
      if (data !== null) return null; // contract: single-line JSON data
      data = value;
    }
    // Other SSE fields (id, retry) are ignored by design.
  }
  if (name === null || data === null) return null;
  return { name, data };
}

export async function streamAssistantAnswer(opts: StreamOptions): Promise<StreamTerminal> {
  const url = (opts.baseUrl || '') + `/api/conversations/${encodeURIComponent(opts.conversationId)}/messages`;
  let res: Response;
  try {
    res = await fetch(url, {
      method: 'POST',
      cache: 'no-store',
      headers: {
        'Content-Type': 'application/json',
        Accept: 'text/event-stream',
        Authorization: `Bearer ${opts.token}`,
      },
      body: JSON.stringify({ text: opts.question }),
      signal: opts.signal,
    });
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') throw err;
    throw new StreamError(0, 'network_error', true);
  }

  // Pre-stream failures stay plain JSON: surface status + code to the caller.
  if (!res.ok) {
    let code = `http_${res.status}`;
    try {
      const bodyText = await res.text();
      const parsed: unknown = JSON.parse(bodyText);
      if (isRecord(parsed) && typeof parsed['error'] === 'string') code = parsed['error'];
    } catch {
      // keep http_* fallback
    }
    const retryable = res.status === 503 || res.status === 0;
    throw new StreamError(res.status, code, retryable);
  }

  const ct = res.headers.get('content-type') || '';
  if (!ct.includes('text/event-stream')) {
    throw new StreamError(0, 'protocol_error', false);
  }
  if (!res.body) {
    throw new StreamError(0, 'protocol_error', false);
  }

  const guard = makeOrderGuard();
  const decoder = new TextDecoder('utf-8');
  const reader = res.body.getReader();
  let buffer = '';
  let terminal: StreamTerminal | null = null;

  const handleFrame = (frame: string): void => {
    const parsed = parseFrame(frame);
    if (!parsed) throw new StreamError(0, 'protocol_error', false);
    const event = parseEvent(parsed.name, parsed.data);
    if (!event) throw new StreamError(0, 'protocol_error', false);
    guard(event);
    if (event.type === 'done') {
      terminal = { status: 'completed', assistantMessageId: event.assistant_message_id, citations: event.citations };
      return;
    }
    if (event.type === 'error') {
      // The server's explicit flag is authoritative; the code table only
      // fills in when the flag is absent.
      terminal = {
        status: 'error',
        code: event.code,
        message: event.message,
        retryable: event.retryable === null ? RETRYABLE_CODES.has(event.code) : event.retryable,
      };
      return;
    }
    opts.onEvent(event);
  };

  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      const { frames, rest } = splitFrames(buffer);
      buffer = rest;
      for (const frame of frames) handleFrame(frame);
    }
    buffer += decoder.decode();
    // Flush a final frame if the server forgot the trailing blank line.
    if (buffer.trim() !== '') {
      handleFrame(buffer);
      buffer = '';
    }
  } catch (err) {
    if (err instanceof StreamError) throw err;
    if (err instanceof DOMException && err.name === 'AbortError') throw err;
    throw new StreamError(0, 'network_error', true);
  } finally {
    reader.releaseLock();
  }

  if (terminal) return terminal;
  // Connection closed without any terminal event.
  return { status: 'error', code: 'stream_closed', message: 'stream_closed', retryable: true };
}
