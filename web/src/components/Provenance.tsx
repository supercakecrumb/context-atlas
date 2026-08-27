import { Anchor, Group, Stack, Text } from '@mantine/core';
import type { DatasetRelease, SnapshotRef } from '../api/types';

export function Provenance({ snapshot, releases }: { snapshot?: SnapshotRef; releases?: DatasetRelease[] }) {
  if (!snapshot) return null;
  return (
    <Stack className="metadata-row" gap={3} mt="md">
      <Group gap="xs"><Text size="xs" fw={700}>Resolved snapshot</Text><Text size="xs" ff="monospace">{snapshot.id}</Text></Group>
      {releases?.map((release) => <Text size="xs" key={release.id}>WHO source accessed {new Date(release.accessed_at).toLocaleDateString('en-GB')} · {release.citation}</Text>)}
      {releases?.length ? <details>
        <summary>Release details</summary>
        <Stack gap={4} mt="xs">
          {releases.map((release) => <Stack gap={1} key={`${release.id}-details`}>
            <Text size="xs">Release <Text component="span" ff="monospace" size="xs">{release.id}</Text> · accessed {new Date(release.accessed_at).toLocaleString('en-GB')}</Text>
            <Anchor href={release.source_url} target="_blank" rel="noreferrer" size="xs">WHO source URL</Anchor>
            <Text size="xs">SHA-256 <Text component="span" ff="monospace" size="xs">{release.sha256}</Text> · parser {release.parser_version}</Text>
          </Stack>)}
        </Stack>
      </details> : null}
    </Stack>
  );
}
