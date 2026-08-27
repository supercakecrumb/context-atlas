import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { formatDisplayValue, PublishedValue } from './PublishedValue';

describe('published value formatting', () => {
  it('keeps four useful significant digits, including for small values', () => {
    expect(formatDisplayValue('17.34344471', 17.34344471)).toBe('17.34');
    expect(formatDisplayValue('0.00014814', 0.00014814)).toBe('0.0001481');
    expect(formatDisplayValue('SUPPRESSED', null)).toBe('SUPPRESSED');
  });

  it('keeps the exact published value available to assistive technology', () => {
    render(<PublishedValue displayValue="17.34344471" numericValue={17.34344471} />);
    expect(screen.getByText('17.34')).toHaveAccessibleName('Exact published value: 17.34344471');
  });
});
