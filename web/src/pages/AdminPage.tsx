import { Alert, Badge, Button, Card, Container, Divider, Group, List, SimpleGrid, Stack, Text, TextInput, Title } from '@mantine/core';
import { useForm } from '@mantine/form';
import { notifications } from '@mantine/notifications';
import { CheckCircle2, Clock3, LogOut, RefreshCw, ShieldCheck, Upload } from 'lucide-react';
import { Link } from 'react-router';
import { useState } from 'react';
import { useAdminSession, useConfirmImport, useFreshness, useImportPreview, useImportRuns, useLogout, usePreviewImport, useRefresh } from '../api/queries';
import { QueryError, QueryLoading } from '../components/AsyncState';
import type { ImportPreview } from '../api/types';

const whoIndicatorPage = /^https:\/\/data\.who\.int\/indicators\/i\/(?:[A-Za-z0-9]{7}\/)?[A-Za-z0-9]{7}$/;

export const isWHOIndicatorPage = (value: string) => whoIndicatorPage.test(value);

export function AdminPage() {
  const session = useAdminSession();
  const runs = useImportRuns(Boolean(session.data));
  const freshness = useFreshness(Boolean(session.data));
  const [previewID, setPreviewID] = useState<string>();
  const previewRequest = usePreviewImport();
  const preview = useImportPreview(previewID);
  const confirm = useConfirmImport();
  const refresh = useRefresh();
  const logout = useLogout();
  const form = useForm({
    initialValues: { source_url: '' },
    validate: { source_url: (value) => isWHOIndicatorPage(value) ? null : 'Enter a canonical HTTPS WHO indicator page URL.' },
  });

  if (session.isPending) return <Container size="md" className="page-section"><QueryLoading label="Checking owner authorization…" /></Container>;
  if (session.isError) return <Container size="md" className="page-section"><QueryError error={session.error} title="Owner authorization could not be checked." /></Container>;
  if (!session.data) return <Container size="md" className="page-section"><Stack gap="lg"><div><Text className="editorial-kicker">Admin</Text><Title className="display-title">Owner-only data controls.</Title></div><Alert icon={<ShieldCheck size={18} />} color="yellow" title="Telegram owner login is required.">Public readers cannot import, confirm releases, or refresh the catalog.</Alert><Button component={Link} to="/login" w="fit-content">Open owner login</Button></Stack></Container>;

  const submitPreview = form.onSubmit(async ({ source_url }) => {
    try {
      const staged = await previewRequest.mutateAsync(source_url);
      setPreviewID(staged.id);
      notifications.show({ message: 'Import preview created. Review it before confirmation.', color: 'teal' });
    } catch {
      notifications.show({ message: 'Preview could not be started.', color: 'red' });
    }
  });
  const refreshNow = async () => {
    try {
      await refresh.mutateAsync();
      notifications.show({ message: 'Refresh queued. Existing releases remain active until a complete replacement succeeds.', color: 'teal' });
    } catch {
      notifications.show({ message: 'Refresh could not be queued.', color: 'red' });
    }
  };
  const confirmPreview = async () => {
    if (!preview.data) return;
    try {
      await confirm.mutateAsync(preview.data.id);
      notifications.show({ message: 'Immutable release and snapshot confirmed.', color: 'teal' });
    } catch {
      notifications.show({ message: 'Release confirmation failed.', color: 'red' });
    }
  };
  const logoutNow = async () => {
    try {
      await logout.mutateAsync();
      notifications.show({ message: 'Owner session ended.', color: 'teal' });
    } catch {
      notifications.show({ message: 'Could not end the owner session.', color: 'red' });
    }
  };

  return <Container size="xl" className="page-section"><Stack gap="xl">
    <Group justify="space-between" align="end"><div><Text className="editorial-kicker">Admin</Text><Title className="display-title">Import carefully, preserve every release.</Title></div><Group><Button leftSection={<LogOut size={16} aria-hidden />} variant="subtle" color="dark" loading={logout.isPending} onClick={logoutNow}>Log out</Button><Button leftSection={<RefreshCw size={16} aria-hidden />} variant="light" color="dark" loading={refresh.isPending} onClick={refreshNow}>Refresh datasets</Button></Group></Group>
    <SimpleGrid cols={{ base: 1, md: 2 }} spacing="lg"><Card withBorder padding="lg"><form onSubmit={submitPreview}><Stack><div><Title order={2} size="h3">Preview WHO import</Title><Text size="sm" c="dimmed">The importer accepts canonical WHO indicator pages only, then discovers the approved download source under strict limits.</Text></div><TextInput label="WHO indicator page" placeholder="https://data.who.int/indicators/i/.../..." required {...form.getInputProps('source_url')} /><Button type="submit" leftSection={<Upload size={16} aria-hidden />} loading={previewRequest.isPending}>Create staged preview</Button></Stack></form></Card><Card withBorder padding="lg"><Stack><Title order={2} size="h3">Refresh policy</Title><List size="sm" spacing="xs"><List.Item>Daily refresh covers every active dataset, including confirmed generic WHO imports.</List.Item><List.Item>Daily refresh begins at 02:15 UTC with a PostgreSQL advisory lock.</List.Item><List.Item>A failed dataset keeps its last known-good release in the next snapshot.</List.Item><List.Item>Unchanged checksums do not create a new snapshot.</List.Item><List.Item>Preview artifacts expire after 24 hours if not confirmed.</List.Item></List></Stack></Card></SimpleGrid>
    {previewRequest.isError && <QueryError error={previewRequest.error} title="The import preview could not be started." />}
    {preview.isError && <QueryError error={preview.error} title="The staged import preview could not be refreshed." />}
    {preview.data && <ImportPreviewCard onConfirm={confirmPreview} confirming={confirm.isPending} preview={preview.data} />}
    <Card withBorder padding="lg"><Stack gap="sm"><div><Title order={2} size="h3">Dataset freshness</Title><Text size="sm" c="dimmed">A stale dataset remains visible from its last good release while the next refresh is investigated.</Text></div>{freshness.isPending ? <QueryLoading label="Loading freshness…" /> : freshness.isError ? <QueryError error={freshness.error} title="Freshness could not be loaded." /> : <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>{freshness.data?.datasets.map((dataset) => <div className="metadata-row" key={dataset.dataset_id}><Group justify="space-between"><Text fw={600} size="sm">{dataset.dataset_id}</Text><Badge color={dataset.stale ? 'red' : 'teal'}>{dataset.stale ? 'stale' : 'current'}</Badge></Group><Text size="xs" c="dimmed" mt={4}>Last success: {dataset.last_success_at ? new Date(dataset.last_success_at).toLocaleString('en-GB') : 'None'}</Text><Text size="xs" c="dimmed">Last attempt: {dataset.last_attempt_state}</Text></div>)}</SimpleGrid>}</Stack></Card>
    <Divider />
    <div><Title order={2}>Recent import runs</Title><Text size="sm" c="dimmed">Interrupted jobs are recovered as failures and can be retried through the same importer path.</Text></div>
    {runs.isPending ? <QueryLoading label="Loading import history…" /> : runs.isError ? <QueryError error={runs.error} title="Import history could not be loaded." /> : <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>{runs.data?.runs.map((run) => <Card key={run.id} withBorder><Group justify="space-between"><Badge color={run.status === 'succeeded' ? 'teal' : run.status === 'failed' ? 'red' : 'blue'}>{run.status}</Badge><Text size="xs" c="dimmed">{run.kind}</Text></Group><Text fw={600} mt="sm">{run.dataset_id ?? 'Catalog refresh'}</Text><Text size="sm" c="dimmed" mt={4}>{new Date(run.started_at).toLocaleString('en-GB')}</Text>{run.error && <Text size="sm" c="red" mt="sm">{run.error}</Text>}</Card>)}</SimpleGrid>}
  </Stack></Container>;
}

function ImportPreviewCard({ preview, onConfirm, confirming }: { preview: ImportPreview; onConfirm: () => void; confirming: boolean }) {
  const ready = preview.status === 'ready';
  return <Card withBorder padding="lg"><Stack><Group justify="space-between"><div><Title order={2} size="h3">Staged import preview</Title><Text size="sm" c="dimmed">Expires {new Date(preview.expires_at).toLocaleString('en-GB')}</Text></div><Badge color={ready ? 'teal' : preview.status === 'failed' ? 'red' : 'yellow'}>{preview.status}</Badge></Group><SimpleGrid cols={{ base: 2, sm: 4 }}>{[['Rows read', preview.rows.source_rows], ['Accepted rows', preview.rows.accepted_rows], ['Duplicates collapsed', preview.rows.collapsed_duplicates], ['Unmapped areas', preview.unmapped_geographies.length]].map(([label, value]) => <div className="metadata-row" key={String(label)}><Text size="xs" c="dimmed">{label}</Text><Text fw={700}>{value}</Text></div>)}</SimpleGrid><Text size="sm"><b>Columns:</b> {preview.headers.join(', ')}</Text><Text size="sm"><b>Measures:</b> {preview.measures.map((measure) => measure.name).join(', ')}</Text><Text size="sm"><b>Dimensions:</b> {preview.dimensions.map((dimension) => dimension.name).join(', ') || 'None'}</Text>{preview.warnings.length > 0 && <Alert color="yellow" title="Preview warnings">{preview.warnings.join(' ')}</Alert>}{ready && <Button leftSection={<CheckCircle2 size={16} aria-hidden />} loading={confirming} onClick={onConfirm}>Confirm immutable release and snapshot</Button>}<Text size="xs" c="dimmed"><Clock3 size={13} aria-hidden /> Confirmation preserves the raw artifact, checksum, parser version, row accounting, citation, and source URL.</Text></Stack></Card>;
}
