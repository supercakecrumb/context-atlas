import { defineConfig } from 'orval';

export default defineConfig({
  contextAtlas: {
    input: {
      target: '../api/openapi.json',
    },
    output: {
      client: 'react-query',
      mode: 'tags-split',
      target: 'src/api/generated/client.ts',
      schemas: 'src/api/generated/models',
      mock: false,
      override: {
        mutator: {
          path: 'src/api/client.ts',
          name: 'request',
        },
      },
    },
  },
});
