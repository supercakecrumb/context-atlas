import { MantineProvider } from '@mantine/core';
import { Notifications } from '@mantine/notifications';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render } from '@testing-library/react';
import { BrowserRouter } from 'react-router';
import { NuqsAdapter } from 'nuqs/adapters/react-router/v8';
import { App } from '../App';
import { atlasTheme } from '../theme';

export function renderApp(path = '/') {
  window.history.replaceState({}, '', path);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 30_000 } } });
  return render(<MantineProvider theme={atlasTheme}><Notifications /><QueryClientProvider client={client}><BrowserRouter><NuqsAdapter><App /></NuqsAdapter></BrowserRouter></QueryClientProvider></MantineProvider>);
}
