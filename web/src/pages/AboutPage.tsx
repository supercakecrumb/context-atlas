import { Anchor, Container, Divider, List, Stack, Text, Title } from '@mantine/core';

export function AboutPage() {
  return (
    <Container size="md" className="page-section">
      <Stack gap="xl">
        <div><Text className="editorial-kicker">Methodology</Text><Title className="display-title">Read the data with its limits.</Title></div>
        <Text className="lede">Context Atlas republishes selected WHO datasets for exploration, retaining dimensions, source geographies, published values, bounds, status, releases, and the exact source citation.</Text>
        <Divider />
        <section><Title order={2}>Exact-year policy</Title><Text mt="sm">A chosen year means that exact published year. The atlas never interpolates a line, replaces a missing year with the nearest available one, or quietly changes either axis of an association.</Text></section>
        <section><Title order={2}>Countries, areas, and groups</Title><Text mt="sm">WHO source geographies are preserved. Maps and associations use numeric, published rows linked to leaf UN M49 countries and areas. UN regions, classifications, and the custom ex-Soviet group filter selections; they never generate synthetic averages. Historical boundary reconstruction is outside this release.</Text></section>
        <section><Title order={2}>Uncertainty and absence</Title><Text mt="sm">When WHO publishes lower and upper bounds, the atlas shows them. Missing, suppressed, not-applicable, and zero values remain different states. Unmapped source areas remain in tables and CSV exports but are not guessed onto a map.</Text></section>
        <section><Title order={2}>Sources and licensing</Title><List mt="sm" spacing="xs"><List.Item>WHO data follows the terms and citation supplied with each source release, commonly CC BY 4.0. No endorsement is implied.</List.Item><List.Item>UN M49 supplies the current classification hierarchy applied across historical years.</List.Item><List.Item>Map geometry is based on Natural Earth Admin-0 50m data, under Natural Earth’s public-domain terms.</List.Item></List></section>
        <Text size="sm" c="dimmed">For suicide-related information, see the <Anchor href="https://www.who.int/news-room/questions-and-answers/item/suicide" target="_blank" rel="noreferrer">WHO suicide Q&amp;A</Anchor>.</Text>
      </Stack>
    </Container>
  );
}
