import { screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../test/render';

describe('home gallery', () => {
  it('starts with the live gallery and gives each rendered preset a complete explorer URL', async () => {
    renderApp('/');
    expect(await screen.findByRole('heading', { name: 'Four compact views, backed by this snapshot' })).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Health data needs its context.' })).not.toBeInTheDocument();
    expect(await screen.findByText('Suicide mortality')).toBeInTheDocument();
    expect(screen.getByText('Road traffic mortality')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole('link', { name: 'Explore Alcohol and suicide' })).toHaveAttribute('href', '/explore?view=association&x_series=alcohol-total&x_year=2001&y_series=suicide-total&y_year=2001&snapshot=snapshot-2026-08-27'));
    expect(screen.getByRole('link', { name: 'Explore AWaRe composition' })).toHaveAttribute('href', '/explore?view=composition&series=aware-access&year=2001&snapshot=snapshot-2026-08-27');
  });
});
