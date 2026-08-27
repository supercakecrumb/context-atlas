const numberFormatter = new Intl.NumberFormat('en-US', { maximumSignificantDigits: 4 });

export function formatNumber(value: number | null | undefined): string {
  return value === null || value === undefined ? '—' : numberFormatter.format(value);
}

export function formatDisplayValue(displayValue: string, numericValue: number | null | undefined): string {
  return numericValue === null || numericValue === undefined ? displayValue : formatNumber(numericValue);
}

export function PublishedValue({ displayValue, numericValue }: { displayValue: string; numericValue: number | null | undefined }) {
  const formatted = formatDisplayValue(displayValue, numericValue);
  if (formatted === displayValue) return <>{formatted}</>;
  const exact = `Exact published value: ${displayValue}`;
  return <span aria-label={exact} title={exact}>{formatted}</span>;
}
