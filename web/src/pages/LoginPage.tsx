import { Alert, Anchor, Card, Container, List, Stack, Text, Title } from '@mantine/core';
import { useAdminSession } from '../api/queries';
import { QueryLoading } from '../components/AsyncState';

export function LoginPage() {
  const session = useAdminSession();
  if (session.isPending) return <Container size="sm" className="page-section"><QueryLoading label="Checking owner session…" /></Container>;
  if (session.data) return <Container size="sm" className="page-section"><Alert color="teal" title="You are signed in as the owner."><Anchor href="/admin">Open import administration.</Anchor></Alert></Container>;

  return (
    <Container size="sm" className="page-section">
      <Stack gap="lg"><div><Text className="editorial-kicker">Owner access</Text><Title className="display-title">Telegram login</Title></div>
        <Card withBorder padding="xl" radius="md"><Stack><Text>Imports and refreshes are restricted to the configured Aurora owner account. Public exploration never requires an account.</Text><List spacing="sm"><List.Item>Open the dedicated Context Atlas Telegram bot in a private chat.</List.Item><List.Item>Send <code>/login</code> and open the one-use link it returns within ten minutes.</List.Item><List.Item>The secure session lasts seven days on this browser.</List.Item></List><Alert color="gray" title="Telegram owner login">Use the private-chat <code>/login</code> command with the configured bot.</Alert></Stack></Card>
        <Text size="sm" c="dimmed">Only the configured Telegram owner ID is authorized. Login links are single-use and expire after ten minutes.</Text>
      </Stack>
    </Container>
  );
}
