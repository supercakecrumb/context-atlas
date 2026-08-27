import { afterEach, describe, expect, it, vi } from 'vitest';
import { request, setCSRFToken } from './client';

describe('Orval request mutator', () => {
  afterEach(() => {
    setCSRFToken(undefined);
    vi.unstubAllGlobals();
  });

  it('returns Orval’s response envelope and keeps same-origin CSRF credentials', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ ready: true }), {
      status: 202,
      headers: { 'Content-Type': 'application/json' },
    }));
    vi.stubGlobal('fetch', fetchMock);
    setCSRFToken('test-token');

    const response = await request<{ data: { ready: boolean }; status: number; headers: Headers }>('/api/v1/test', {
      body: JSON.stringify({ value: 1 }),
      method: 'POST',
    });

    expect(response.data).toEqual({ ready: true });
    expect(response.status).toBe(202);
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/test', expect.objectContaining({
      credentials: 'same-origin',
      headers: expect.objectContaining({ get: expect.any(Function) }),
    }));
    const requestHeaders = fetchMock.mock.calls[0][1].headers as Headers;
    expect(requestHeaders.get('X-CSRF-Token')).toBe('test-token');
  });
});
