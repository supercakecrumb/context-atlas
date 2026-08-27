import { AppShell, Burger, Container, Drawer, Group, Stack } from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { BookOpenText, Database, Info } from 'lucide-react';
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
            <strong>Context Atlas</strong>
          </NavLink>
          <Group className="header-nav" gap="xs" visibleFrom="sm">{links}</Group>
          <Burger className="header-burger" opened={opened} onClick={toggle} aria-label="Toggle navigation" hiddenFrom="sm" />
        </Container>
      </AppShell.Header>
      <Drawer opened={opened} onClose={close} title="Context Atlas" hiddenFrom="sm" padding="lg">
        <Stack gap="xs">{links}</Stack>
      </Drawer>
      <AppShell.Main>
        <Outlet />
      </AppShell.Main>
      <FeedbackButton />
    </AppShell>
  );
}
