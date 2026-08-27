import { Alert, Center, Loader, Stack, Text } from '@mantine/core';
import { CircleAlert } from 'lucide-react';
import type { ReactNode } from 'react';
import { ApiError } from '../api/client';

export function QueryLoading({ label = 'Loading data…' }: { label?: string }) {
  return <Center mih={180}><Stack align="center"><Loader color="dark" /><Text c="dimmed">{label}</Text></Stack></Center>;
}

export function QueryError({ error, title = 'Data could not be loaded.' }: { error: unknown; title?: string }) {
  const detail = error instanceof ApiError ? error.detail : undefined;
  return <Alert icon={<CircleAlert size={18} />} color="red" title={title}>{detail ?? 'Try again shortly. No values have been substituted.'}</Alert>;
}

export function EmptyState({ title, children }: { title: string; children?: ReactNode }) {
  return <Alert color="gray" title={title}>{children}</Alert>;
}
