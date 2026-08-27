import { Button, Container, Stack, Text, Title } from '@mantine/core';
import { Link } from 'react-router';

export function NotFoundPage() {
  return <Container size="sm" className="page-section"><Stack><Text className="editorial-kicker">404</Text><Title className="display-title">That page is not in this atlas.</Title><Text c="dimmed">Try the gallery or open the explorer.</Text><Button component={Link} to="/" w="fit-content">Back to the atlas</Button></Stack></Container>;
}
