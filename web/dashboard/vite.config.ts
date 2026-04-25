import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [sveltekit()],
  server: {
    allowedHosts: ['lab'],
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:18080',
        rewrite: (path) => path.replace(/^\/api/, '')
      }
    }
  }
});
