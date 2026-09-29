// API client for the CampusClaw backend.
// All requests send cookies (credentials: 'include') so the httpOnly session
// cookie is transported automatically. JavaScript cannot read the cookie.

export type Role = 'teacher' | 'student';

export interface Me {
  id: number;
  username: string;
  role: Role;
  class_id: number | null;
}

export interface Material {
  id: number;
  class_id: number;
  uploader_id: number;
  uploader: string;
  title: string;
  original_filename: string;
  created_at: string;
  content: string;
}

export interface MaterialsList {
  materials: Material[];
}

export interface UploadResult {
  id: number;
  status: string;
}

export interface UploadProgress {
  loaded: number;
  total: number;
  percent: number;
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
    res = await fetch(path, {
      credentials: 'include',
      cache: 'no-store',
      ...init,
      headers: {
        Accept: 'application/json',
        ...(init?.headers || {}),
      },
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

export function login(username: string, password: string): Promise<{ status: string }> {
  return request<{ status: string }>('/api/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  });
}

export function logout(): Promise<void> {
  return request<void>('/api/logout', { method: 'POST' });
}

export function fetchMaterials(q?: string): Promise<Material[]> {
  const url = new URL('/api/materials', window.location.origin);
  if (q && q.trim()) url.searchParams.set('q', q.trim());
  return request<MaterialsList>(url.toString()).then((d) => d.materials);
}

export function fetchMaterial(id: number): Promise<Material> {
  return request<Material>(`/api/materials/${id}`);
}

// The browser will fetch the file with cookies automatically (same-origin).
export function materialFileUrl(id: number): string {
  return `/api/materials/${id}/file`;
}

// Upload uses XHR so we can report byte-level progress. fetch() cannot.
export function uploadMaterial(opts: {
  file: File;
  onProgress?: (p: UploadProgress) => void;
  signal?: AbortSignal;
}): Promise<UploadResult> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/api/materials');
    xhr.withCredentials = true;
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
    xhr.send(form);
  });
}
