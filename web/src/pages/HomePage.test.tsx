import { screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../test/render';

describe('home gallery', () => {
  it('shows all six launch datasets and the explorer entry point', async () => {
    renderApp('/');
    expect(screen.getByRole('heading', { name: 'Health data needs its context.' })).toBeInTheDocument();
    expect(await screen.findByText('Suicide mortality')).toBeInTheDocument();
    expect(screen.getByText('Road traffic mortality')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole('link', { name: /start exploring/i })).toHaveAttribute('href', '/explore?snapshot=snapshot-2026-08-27'));
  });
});
