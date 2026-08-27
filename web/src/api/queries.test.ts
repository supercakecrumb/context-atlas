import { afterEach, describe, expect, it, vi } from 'vitest';
import { catalog } from '../test/fixtures';
import { fetchFreshLatestCatalog } from './queries';

describe('fresh latest catalog resolution', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('requests the unpinned catalog with no-store before pinning it', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(catalog), {
      headers: { 'Content-Type': 'application/json' },
      status: 200,
    }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(fetchFreshLatestCatalog()).resolves.toMatchObject({ meta: catalog.meta });
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/catalog', expect.objectContaining({
      cache: 'no-store',
      method: 'GET',
    }));
  });
});
