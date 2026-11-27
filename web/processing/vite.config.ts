import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

// vite.config.ts runs in Node — declare the global so tsc (without @types/node)
// can still compile this file under tsconfig.node.json's bare lib.
declare const process: { env: Record<string, string | undefined> };

// Dev proxy: inject PROCESSING_BLUEI_OPERATOR_TOKEN from env so the app can call
// protected /v1/* endpoints without a token UI.
const operatorToken: string = process.env.PROCESSING_BLUEI_OPERATOR_TOKEN ?? '';
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const injectAuth = (proxy: any) => {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  proxy.on('proxyReq', (proxyReq: any) => {
    if (operatorToken) proxyReq.setHeader('Authorization', 'Bearer ' + operatorToken);
  });
};

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5174,
    strictPort: true,
    proxy: {
      '/v1':      { target: 'http://127.0.0.1:8080', changeOrigin: true, configure: injectAuth },
      '/healthz': { target: 'http://127.0.0.1:8080', changeOrigin: true, configure: injectAuth },
    },
  },
  envPrefix: ['VITE_'],
});
