import '@fontsource-variable/noto-sans-kr';
import '@mantine/core/styles.css';
import '@mantine/notifications/styles.css';
import './styles.css';

import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MantineProvider, createTheme } from '@mantine/core';
import { Notifications } from '@mantine/notifications';
import App from './App';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, staleTime: 15_000, refetchOnWindowFocus: false },
  },
});

const theme = createTheme({
  primaryColor: 'teal',
  primaryShade: { light: 7, dark: 5 },
  fontFamily: '"Noto Sans KR Variable", "Noto Sans KR", sans-serif',
  headings: { fontFamily: '"Noto Sans KR Variable", "Noto Sans KR", sans-serif', fontWeight: '750' },
  defaultRadius: 'md',
  fontSizes: { xs: '0.8125rem', sm: '0.9375rem', md: '1rem', lg: '1.125rem', xl: '1.25rem' },
  colors: {
    navy: ['#f2f7fa', '#e2edf4', '#bfd3e2', '#99b8d0', '#789fbe', '#648eb4', '#5985b0', '#496f9c', '#3b628c', '#102a43'],
    teal: ['#e7fffd', '#d4faf7', '#aaf2ed', '#7ceae3', '#57e3db', '#40ded5', '#2edbd1', '#1bc3ba', '#11b5ae', '#008f89'],
  },
  components: {
    Button: { defaultProps: { size: 'md' } },
    TextInput: { defaultProps: { size: 'md' } },
    PasswordInput: { defaultProps: { size: 'md' } },
    Select: { defaultProps: { size: 'md' } },
    Table: { defaultProps: { verticalSpacing: 'md', horizontalSpacing: 'md' } },
  },
});

createRoot(document.getElementById('root')!).render(
  <MantineProvider theme={theme} defaultColorScheme="light">
    <Notifications position="top-right" />
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </QueryClientProvider>
  </MantineProvider>,
);
