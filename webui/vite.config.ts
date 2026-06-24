import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  root: 'web',
  base: './',
  plugins: [svelte({ preprocess: vitePreprocess({ script: true }) })],
  build: {
    outDir: '../dist',
    emptyOutDir: true,
    target: 'es2022',
  },
});
