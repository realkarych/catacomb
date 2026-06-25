import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  plugins: [svelte()],
  test: {
    environment: 'jsdom',
    include: ['web/src/**/*.{test,spec}.{ts,svelte.ts}'],
    exclude: ['e2e/**', 'node_modules/**'],
    passWithNoTests: true,
    coverage: {
      provider: 'v8',
      include: [
        'web/src/lib/reducer/**',
        'web/src/lib/stores/selectors.ts',
        'web/src/lib/format/**',
        'web/src/lib/pricing/**',
        'web/src/lib/sse/client.ts',
        'web/src/lib/api.ts',
        'web/src/lib/payload-view.ts',
        'web/src/lib/conversation.ts',
        'web/src/lib/router.ts',
        'web/src/lib/sessions-sort.ts',
        'web/src/lib/layout.ts',
        'web/src/lib/node-legend.ts',
        'web/src/lib/timeline.ts',
        'web/src/lib/filters.ts',
        'web/src/lib/graph-nav.ts',
        'web/src/lib/graph/**',
      ],
      exclude: [
        '**/*.test.ts',
        '**/*.spec.ts',
        'web/src/lib/stores/stores.svelte.ts',
        'web/src/**/*.svelte',
      ],
      thresholds: {
        100: true,
        perFile: true,
      },
      reporter: ['text', 'text-summary'],
    },
  },
});
