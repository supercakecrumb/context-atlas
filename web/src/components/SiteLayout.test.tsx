import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../test/render';

describe('site layout', () => {
  it('keeps owner controls out of the visible navigation', () => {
    renderApp('/');

    expect(document.querySelector('.brand-mark')).not.toBeInTheDocument();
    expect(screen.queryByText('WHO data, in context')).not.toBeInTheDocument();
    expect(screen.queryByText(/WHO data retains its source-specific terms/)).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Owner login' })).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Admin' })).not.toBeInTheDocument();
  });

  it.each([
    ['/login', 'Telegram owner login'],
    ['/admin', 'Owner-only data controls.'],
  ])('keeps %s reachable directly', async (path, text) => {
    renderApp(path);

    expect(await screen.findByText(text)).toBeInTheDocument();
  });
});
