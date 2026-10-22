import type {MCPCall} from './sse';
// API client for the CampusClaw backend. The opaque access token is scoped to
// the current browser tab and sent on protected requests as a Bearer token.

const TOKEN_KEY = 'campusclaw-access-token';
let memoryAccessToken: string | null = null;

export function getAccessToken(): string | null {
  try {
    memoryAccessToken = sessionStorage.getItem(TOKEN_KEY);
  } catch {
    // Fall back to memory when browser storage is unavailable.
  }
  return memoryAccessToken;
}

function setAccessToken(token: string): void {
  memoryAccessToken = token;
  try {
    sessionStorage.setItem(TOKEN_KEY, token);
  } catch {
    // The token remains usable in memory for the current page.
  }
}

export function clearAccessToken(): void {
  memoryAccessToken = null;
  try {
    sessionStorage.removeItem(TOKEN_KEY);
  } catch {
    // Ignore unavailable browser storage while clearing local auth state.
  }
}

export type Role = 'teacher' | 'student';

export interface Me {
  id: number;
  username: string;
  role: Role;
  class_id: number | null;
}

export interface Material {
  id: string;
  class_id: number;
  uploader_id: number;
  uploader: string;
  title: string;
  original_filename: string;
  created_at: string;
  content: string;
	index_status: 'ready' | 'pending' | 'failed' | 'missing';
}

export interface MaterialsList {
  materials: Material[];
}

export interface UploadResult {
	id: string;
  status: string;
	index_status: string;
}

export interface UploadProgress {
  loaded: number;
  total: number;
  percent: number;
}

export interface LoginResult {
  access_token: string;
  token_type: 'Bearer';
  expires_in: number;
}

// ApiError carries the HTTP status so callers can branch on 401/403/404/503.
export class ApiError extends Error {
  status: number;
  code: string;
  body: unknown;
  constructor(status: number, code: string, body?: unknown) {
    super(code || `HTTP ${status}`);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.body = body;
  }
}

function extractCode(body: unknown): string {
  if (body && typeof body === 'object' && 'error' in body) {
    return String((body as Record<string, unknown>).error);
  }
  return '';
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response;
  try {
    const headers = new Headers(init?.headers);
    headers.set('Accept', 'application/json');
    const token = getAccessToken();
    if (token) headers.set('Authorization', `Bearer ${token}`);
    res = await fetch(path, {
      cache: 'no-store',
      ...init,
      headers,
    });
  } catch {
    // Network failure / unreachable backend. Treat like service unavailable
    // so the UI shows the "service unavailable" state without logging out.
    throw new ApiError(0, 'network_error');
  }

  const ct = res.headers.get('content-type') || '';
  let body: unknown = null;
  if (ct.includes('application/json')) {
    body = await res.json().catch(() => null);
  } else if (res.status >= 400) {
    body = await res.text().catch(() => '');
  }

  if (!res.ok) {
    const code = extractCode(body) || `http_${res.status}`;
    throw new ApiError(res.status, code, body);
  }

  if (res.status === 204) return undefined as T;
  return body as T;
}

export function fetchMe(): Promise<Me> {
  return request<Me>('/api/me');
}

export async function login(username: string, password: string): Promise<LoginResult> {
  const result = await request<LoginResult>('/api/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  });
  if (result.token_type !== 'Bearer' || !result.access_token) {
    throw new ApiError(500, 'invalid_login_response', result);
  }
  setAccessToken(result.access_token);
  return result;
}

export async function logout(): Promise<void> {
  try {
    await request<void>('/api/logout', { method: 'POST' });
  } finally {
    clearAccessToken();
  }
}

export function fetchMaterials(q?: string): Promise<Material[]> {
  const url = new URL('/api/materials', window.location.origin);
  if (q && q.trim()) url.searchParams.set('q', q.trim());
  return request<MaterialsList>(url.toString()).then((d) => d.materials);
}

export function fetchMaterial(id: string): Promise<Material> {
  return request<Material>(`/api/materials/${encodeURIComponent(id)}`);
}

export async function downloadMaterial(id: string): Promise<Blob> {
  const token = getAccessToken();
  let res: Response;
  try {
    res = await fetch(`/api/materials/${id}/file`, {
      cache: 'no-store',
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    });
  } catch {
    throw new ApiError(0, 'network_error');
  }
  if (!res.ok) {
    const ct = res.headers.get('content-type') || '';
    const body = ct.includes('application/json')
      ? await res.json().catch(() => null)
      : await res.text().catch(() => '');
    throw new ApiError(res.status, extractCode(body) || `http_${res.status}`, body);
  }
  return res.blob();
}

// Upload uses XHR so we can report byte-level progress. fetch() cannot.
export function uploadMaterial(opts: {
  file: File;
	strategy?: ChunkStrategy;
	chunkOptions?: ChunkOptions;
  onProgress?: (p: UploadProgress) => void;
  signal?: AbortSignal;
}): Promise<UploadResult> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/api/materials');
    const token = getAccessToken();
    if (token) xhr.setRequestHeader('Authorization', `Bearer ${token}`);
    xhr.responseType = 'json';

    const report = (loaded: number, total: number) => {
      const percent = total > 0 ? Math.round((loaded / total) * 100) : 0;
      opts.onProgress?.({ loaded, total, percent });
    };

    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) report(e.loaded, e.total);
    };
    // For small files onprogress may not fire; onload guarantees 100%.
    xhr.upload.onload = () => report(opts.file.size, opts.file.size);

    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        let data = xhr.response;
        if (data === null || data === undefined) {
          try {
            data = xhr.responseText ? JSON.parse(xhr.responseText) : {};
          } catch {
            data = {};
          }
        }
        resolve(data as UploadResult);
      } else {
        let body: unknown = xhr.response;
        if (body === null || body === undefined) {
          try {
            body = xhr.responseText ? JSON.parse(xhr.responseText) : null;
          } catch {
            body = xhr.responseText;
          }
        }
        const code = extractCode(body) || `http_${xhr.status}`;
        reject(new ApiError(xhr.status, code, body));
      }
    };

    xhr.onerror = () => reject(new ApiError(0, 'network_error'));
    xhr.onabort = () => reject(new ApiError(0, 'aborted'));

    if (opts.signal) {
      if (opts.signal.aborted) {
        xhr.abort();
      } else {
        opts.signal.addEventListener('abort', () => xhr.abort());
      }
    }

    const form = new FormData();
    form.append('file', opts.file);
	form.append('strategy', opts.strategy || 'auto');
	if (opts.strategy === 'custom' && opts.chunkOptions) {
		form.append('max_length', String(opts.chunkOptions.max_length));
		form.append('overlap_percent', String(opts.chunkOptions.overlap_percent));
		form.append('separator', opts.chunkOptions.separator);
		form.append('remove_urls', String(opts.chunkOptions.remove_urls));
		form.append('remove_emails', String(opts.chunkOptions.remove_emails));
		form.append('collapse_whitespace', String(opts.chunkOptions.collapse_whitespace));
	}
    xhr.send(form);
  });
}

// ---------------------------------------------------------------------------
// Fourth lesson: class-scoped traceable retrieval and grounded answers.
// ---------------------------------------------------------------------------

export type RetrievalMode = 'keyword' | 'vector' | 'hybrid';
export type ChunkStrategy = 'auto' | 'custom' | 'hierarchy';

export interface ChunkOptions {
	max_length: number;
	overlap_percent: number;
	separator: 'newline' | 'blank' | 'period';
	remove_urls: boolean;
	remove_emails: boolean;
	collapse_whitespace: boolean;
}

export interface RetrievalHit {
	chunk_id: number;
	material_id: string;
	material_title: string;
	chunk_index: number;
	char_start: number;
	char_end: number;
	excerpt: string;
	keyword_rank?: number;
	vector_rank?: number;
	final_rank?: number;
	score?: number;
	source_url: string;
}

export interface RetrievalResponse {
	mode: RetrievalMode;
	hits: RetrievalHit[];
}

export interface AskCitation {
	ordinal: number;
	material_id: string;
	material_title: string;
	chunk_id: number;
	chunk_index: number;
	char_start: number;
	char_end: number;
	excerpt: string;
	source_url: string;
}

export interface AskResponse {
	answer: string;
	hits: RetrievalHit[];
	citations: AskCitation[];
}

export function searchKnowledge(query: string, mode: RetrievalMode, limit = 4): Promise<RetrievalResponse> {
	return request<RetrievalResponse>('/api/retrieval/search', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ query, mode, limit }),
	});
}

export function askKnowledge(question: string): Promise<AskResponse> {
	return request<AskResponse>('/api/ask', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ question }),
	});
}

export function rebuildMaterial(id: string, strategy: ChunkStrategy, options?: ChunkOptions): Promise<{ index_status: string }> {
	return request<{ index_status: string }>(`/api/materials/${encodeURIComponent(id)}/reindex`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ strategy, ...(strategy === 'custom' ? options : {}) }),
	});
}

// ---------------------------------------------------------------------------
// Fifth lesson: conversations, assistant prompt and skill management.
// The server always derives owner/class/role from the Bearer token, so none
// of these functions accept client-supplied class or role overrides.
// ---------------------------------------------------------------------------

export interface Conversation {
  agent_id: number;
  agent_name: string;
  id: string;
  title: string;
  created_at: string;
  updated_at: string;
}

export interface Citation {
  ordinal: number;
  material_id: string;
  material_title: string;
  chunk_id: number;
  chunk_index: number;
  char_start: number;
  char_end: number;
  excerpt: string;
}

export type MessageStatus = 'completed' | 'failed' | 'cancelled' | 'streaming';

export interface ConversationMessage {
 tool_calls?: MCPCall[];
  skill_name?: string;
  id: string;
  sequence_no: number;
  role: 'user' | 'assistant';
  content: string;
  status: MessageStatus;
  error_code?: string;
  created_at: string;
  completed_at?: string;
  citations?: Citation[];
}

export interface ConversationDetail {
  conversation: Conversation;
  messages: ConversationMessage[];
}

export function createConversation(agentId: number): Promise<Conversation> {
  return request<Conversation>('/api/conversations', { method: 'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({agent_id:agentId}) });
}

export function fetchConversations(): Promise<Conversation[]> {
  return request<{ conversations: Conversation[] }>('/api/conversations').then(
    (d) => d.conversations,
  );
}

export function fetchConversation(id: string): Promise<ConversationDetail> {
  return request<ConversationDetail>(`/api/conversations/${encodeURIComponent(id)}`);
}

export interface PromptState {
  configured: boolean;
  content?: string;
  version?: number;
  updated_by?: number;
  updated_at?: string;
}

export function fetchPrompt(): Promise<PromptState> {
  return request<PromptState>('/api/assistant/prompt');
}

export function savePrompt(content: string): Promise<PromptState> {
  return request<PromptState>('/api/assistant/prompt', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ content }),
  });
}

export interface SkillState {
  skill_key: string;
  enabled: boolean;
  updated_by?: number;
  updated_at?: string;
}

export function fetchSkill(): Promise<SkillState> {
  return request<SkillState>('/api/assistant/skills/guided-problem-solving');
}

export function saveSkill(enabled: boolean): Promise<SkillState> {
  return request<SkillState>('/api/assistant/skills/guided-problem-solving', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled }),
  });
}


export interface TutorAgent { id:number; name:string; prompt:string; enabled:boolean; version:number; skill_ids:number[]; material_ids:string[]; }
export interface TutorAgentInput {name:string;prompt:string;enabled:boolean;skill_ids:number[];material_ids:string[];}
export interface TutorSkill {id:number;name:string;trigger_text:string;subject_text:string;description:string;tools:string[];tool_policy:'auto'|'required';source_text:string;enabled:boolean;}
export const fetchAgents=():Promise<TutorAgent[]>=>request<{agents:TutorAgent[]}>('/api/agents').then(x=>x.agents);
export const saveAgent=(input:TutorAgentInput,id?:number):Promise<TutorAgent>=>request<TutorAgent>(id?`/api/agents/${id}`:'/api/agents',{method:id?'PUT':'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(input)});
export const fetchSkills=():Promise<TutorSkill[]>=>request<{skills:TutorSkill[]}>('/api/skills').then(x=>x.skills);
export const saveSkillFile=(sourceText:string,id?:number):Promise<TutorSkill>=>request<TutorSkill>(id?`/api/skills/${id}`:'/api/skills',{method:id?'PUT':'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({source_text:sourceText})});
export const deleteSkill=(id:number):Promise<{status:string}>=>request(`/api/skills/${id}`,{method:'DELETE'});
export interface SkillImport { source_text:string; name?:string; trigger_text?:string; subject_text?:string; tools?:string[]; tool_policy?:'auto'|'required'; enabled?:boolean; }
export interface SkillPreview { format:'course'|'agent-skills'; skill:TutorSkill; warnings:string[]; ready:boolean; }
export interface SkillExport { filename:string; directory:string; source_text:string; warnings:string[]; }
export const previewSkill=(input:SkillImport):Promise<SkillPreview>=>request('/api/skills/preview',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(input)});
export const exportSkill=(id:number):Promise<SkillExport>=>request(`/api/skills/${id}/export`);

export interface MCPServer {id:number;name:string;base_url:string;connected:boolean;token_configured:boolean;last_error:string;version:number}
export interface MCPTool {name:string;description:string;inputSchema:Record<string,unknown>;annotations:{readOnlyHint:boolean;destructiveHint:boolean;openWorldHint:boolean};approved:boolean;previous_description?:string}
export type {MCPCall} from './sse';
export interface MCPAudit extends MCPCall {id:number;server_name:string;user_id:number;username:string;conversation_id:string;assistant_message_id:string;created_at:string;latency_ms:number}
export interface MCPChange {id:number;server_name:string;tool_name:string;previous:MCPTool|null;current:MCPTool;approved_at:string|null;created_at:string}
export async function fetchMCPServers(){return (await request<{servers:MCPServer[]}>('/api/mcp/servers')).servers;}
export async function fetchMCPTools(id:number){return (await request<{tools:MCPTool[]}>(`/api/mcp/servers/${id}/tools`)).tools;}
export async function fetchMCPAudit(){return (await request<{tool_calls:MCPAudit[]}>('/api/mcp/tool-calls')).tool_calls;}
export async function fetchMCPChanges(){return (await request<{changes:MCPChange[]}>('/api/mcp/tool-changes')).changes;}
export function saveMCPServer(input:{name:string;base_url:string;auth_token?:string;use_env_token?:boolean},id?:number){return request<MCPServer>(`/api/mcp/servers${id?`/${id}`:''}`,{method:id?'PUT':'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(input)});}
export function mcpAction(id:number,action:'connect'|'disconnect'){return request<MCPServer|{status:string}>(`/api/mcp/servers/${id}/${action}`,{method:'POST'});}
export function approveMCPTool(id:number,name:string){return request(`/api/mcp/servers/${id}/tools/approve`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({names:[name]})});}
export function deleteMCPServer(id:number){return request(`/api/mcp/servers/${id}`,{method:'DELETE'});}
