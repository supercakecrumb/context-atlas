import { AppShell, Burger, Button, Container, Drawer, Group, Stack, Text } from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { BookOpenText, Database, Info, LogIn, ShieldCheck } from 'lucide-react';
import { NavLink, Outlet } from 'react-router';
import { FeedbackButton } from './FeedbackButton';

const navigation = [
  { to: '/', label: 'Atlas', icon: BookOpenText, end: true },
  { to: '/explore', label: 'Explore', icon: Database },
  { to: '/data', label: 'Data', icon: Database },
  { to: '/about', label: 'About', icon: Info },
];

export function SiteLayout() {
  const [opened, { toggle, close }] = useDisclosure(false);

  const links = navigation.map(({ to, label, icon: Icon, end }) => (
    <NavLink key={to} to={to} end={end} className={({ isActive }) => `site-link ${isActive ? 'site-link-active' : ''}`} onClick={close}>
      <Icon size={16} aria-hidden />
      {label}
    </NavLink>
  ));

  return (
    <AppShell header={{ height: 68 }} padding={0}>
      <AppShell.Header className="site-header">
        <Container size="xl" className="header-inner">
          <NavLink to="/" className="brand" aria-label="Context Atlas home">
            <span className="brand-mark">CA</span>
            <span>
              <strong>Context Atlas</strong>
              <small>WHO data, in context</small>
            </span>
          </NavLink>
          <Group gap="xs" visibleFrom="sm">{links}</Group>
          <Group gap="xs" visibleFrom="sm">
            <Button component={NavLink} to="/login" variant="subtle" color="dark" leftSection={<LogIn size={16} aria-hidden />}>Owner login</Button>
            <Button component={NavLink} to="/admin" variant="light" color="dark" leftSection={<ShieldCheck size={16} aria-hidden />}>Admin</Button>
          </Group>
          <Burger opened={opened} onClick={toggle} aria-label="Toggle navigation" hiddenFrom="sm" />
        </Container>
      </AppShell.Header>
      <Drawer opened={opened} onClose={close} title="Context Atlas" hiddenFrom="sm" padding="lg">
        <Stack gap="xs">{links}</Stack>
        <Button component={NavLink} to="/login" variant="subtle" color="dark" mt="md" onClick={close}>Owner login</Button>
        <Button component={NavLink} to="/admin" variant="light" color="dark" mt="xs" onClick={close}>Admin</Button>
      </Drawer>
      <AppShell.Main>
        <Outlet />
        <footer className="site-footer">
          <Container size="xl">
            <Text size="sm">WHO data retains its source-specific terms. Values are displayed exactly as published; years are never interpolated.</Text>
            <Text size="sm" c="dimmed">Countries and areas follow the source and current UN M49 classification. Boundaries are illustrative. Made with Natural Earth.</Text>
          </Container>
        </footer>
      </AppShell.Main>
      <FeedbackButton />
    </AppShell>
  );
}
