import { QueryClient } from '@tanstack/react-query';

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // The connection banner must appear on the first failed poll, not after retries.
      retry: false,
      // Monitoring keeps polling while the tab is hidden.
      refetchIntervalInBackground: true,
      // The loopback server stays reachable when the browser reports itself offline.
      networkMode: 'always',
    },
    mutations: {
      networkMode: 'always',
    },
  },
});
