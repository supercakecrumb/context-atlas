export class ApiError extends Error {
  readonly status: number;
  readonly detail?: string;

  constructor(status: number, message: string, detail?: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.detail = detail;
  }
}

type Problem = { detail?: string; title?: string; message?: string };

let activeCSRFToken: string | undefined;

export function setCSRFToken(token: string | undefined) {
  activeCSRFToken = token;
}

function csrfToken(): string | undefined {
  return activeCSRFToken ?? globalThis.document?.querySelector<HTMLMetaElement>('meta[name="csrf-token"]')?.content;
}

type ResponseEnvelope = { data: unknown; status: number; headers: Headers };

export async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set('Accept', 'application/json, text/csv;q=0.9');

  if (init.body && !(init.body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json');
  }

  if (init.method && !['GET', 'HEAD', 'OPTIONS'].includes(init.method.toUpperCase())) {
    const token = csrfToken();
    if (token) headers.set('X-CSRF-Token', token);
  }

  const response = await fetch(path, {
    ...init,
    headers,
    credentials: 'same-origin',
  });

  if (!response.ok) {
    const problem = await response.json().catch(() => ({} as Problem)) as Problem;
    throw new ApiError(
      response.status,
      problem.title ?? problem.message ?? `Request failed (${response.status})`,
      problem.detail,
    );
  }

  let data: unknown;
  if (response.status !== 204) {
    const contentType = response.headers.get('Content-Type') ?? '';
    data = contentType.includes('json') ? await response.json() : await response.blob();
  }

  // Orval's generated response types include the body, status, and headers.
  return { data, status: response.status, headers: response.headers } as T & ResponseEnvelope;
}
