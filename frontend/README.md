# Media Gate — Frontend

Vue 3 + TypeScript SPA built with Vite and styled with Tailwind CSS v4.

For project-wide documentation (architecture, configuration, deployment) see the [root README](../README.md).

## Prerequisites

- Node.js (see `.nvmrc` or `package.json` engines if present)
- Backend running on `localhost:8080` (Vite proxies `/api` there)

## Scripts

```sh
npm run dev            # Vite dev server with HMR
npm run build          # Type-check + production build
npm run type-check     # vue-tsc --build
npm run lint           # Biome check (formatting + linting)
npm run lint:fix       # Biome auto-fix
npm run generate:api   # Regenerate TS types from OpenAPI spec
```

## Linting & Formatting

Uses [Biome](https://biomejs.dev/) — config in `biome.json`. Note: `noUnusedImports` is disabled for `*.vue` files because Biome cannot see `<template>` usage.

## Code Generation

API types are generated from `../api/openapi.yaml`:

```sh
npm run generate:api   # outputs src/api/schema.d.ts
```

Never hand-edit `src/api/schema.d.ts`.
