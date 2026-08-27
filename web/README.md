# Context Atlas web

```bash
npm run typecheck
npm test
npm run build
npm run test:e2e
```

The app uses React Router 8 through `react-router` and nuqs’ official React Router v8 adapter.
React Router 8 folds browser-DOM exports into `react-router`, so no separate `react-router-dom`
package is installed.

`src/api/generated/` is generated from `../api/openapi.json`; refresh it with `npm run generate:api`.
The small `api/types.ts` and `api/queries.ts` adapters only normalize nullable generated collections
for rendering and add chart/admin polling behavior. They do not declare duplicate wire DTOs.
