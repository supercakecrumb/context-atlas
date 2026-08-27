import { Anchor, Stack, Text } from '@mantine/core';
import type { DatasetRelease, SnapshotRef } from '../api/types';

export function Provenance({ snapshot, releases }: { snapshot?: SnapshotRef; releases?: DatasetRelease[] }) {
  if (!snapshot) return null;
  const sourceLabel = releases?.length ? `${releases.length} source${releases.length === 1 ? '' : 's'}` : 'Source';

  return (
    <Stack gap={2} mt="md">
      <Text size="xs" c="gray.7">{sourceLabel} · snapshot <Text component="span" ff="monospace" size="xs">{snapshot.id}</Text></Text>
      {releases?.length ? <details>
        <summary>Release details</summary>
        <Stack gap={4} mt="xs">
          {releases.map((release) => <Stack gap={1} key={`${release.id}-details`}>
            <Text size="xs">{release.citation}</Text>
            <Text size="xs">Release <Text component="span" ff="monospace" size="xs">{release.id}</Text> · accessed {new Date(release.accessed_at).toLocaleString('en-GB')}</Text>
            <Anchor href={release.source_url} target="_blank" rel="noreferrer" size="xs">Source URL</Anchor>
            <Text size="xs">SHA-256 <Text component="span" ff="monospace" size="xs">{release.sha256}</Text> · parser {release.parser_version}</Text>
          </Stack>)}
        </Stack>
      </details> : null}
    </Stack>
  );
}
