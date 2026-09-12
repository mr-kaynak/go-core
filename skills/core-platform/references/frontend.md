# Frontend and SDK consumers

## Package surfaces

- `@mr-kaynak/core-sdk`: React-free client/auth store/SSE and shared wire types. `createCoreClient({apiUrl, storageKey, defaultHeaders?})` produces isolated state; runtime storage and fetch capabilities still need to exist. Don't assume every CLI or worker has browser localStorage.
- `@mr-kaynak/core-ui`: React components and theme. Import components from package root and prebuilt CSS from `@mr-kaynak/core-ui/styles.css`; Tailwind is not required just to consume that CSS.
- `@mr-kaynak/core-admin`: `createAdminApp({apiUrl, storageKey, basePath?, branding?, modules?})` returns `{Router}`. Mount once; owns router, theme, provider, toast host. Use one live admin per page because built-in feature APIs currently share an active-client registry. Multiple SDK clients are isolated.

`apiUrl` includes `/api/v1`; calls use `/projects`. `storageKey` is required and unique per app. `basePath` defaults `/`; module paths stay `/projects` even at `/panel`. Branding accepts `title` and optional React `logo`.

## Business modules

```tsx
import type { AdminModule } from "@mr-kaynak/core-admin"
import { ProjectsPage } from "./projects-page"

export const projectsModule: AdminModule = {
  name: "projects",
  routes: [{ path: "/projects", element: <ProjectsPage />, requirePermission: "projects.view" }],
  menuItems: [{ label: "My projects", path: "/projects", requirePermission: "projects.view" }],
}
```

Implement `ProjectsPage` in the consumer, then pass `modules: [projectsModule]` to the factory. Route and menu permissions are independent. No implicit built-in admin-only role restriction applies to arbitrary module routes; declare permission explicitly. Route collisions throw in development and drop the colliding route with a log in production. Avoid reserved core routes.

Inside components use `useCoreClient()` and `usePermission("projects.create")`; `useAuth`, `useBranding`, `useCoreRuntime` are also public. Never import the internal active-client singleton. UI grants are presentation, not protection. Test direct URLs, not just menu visibility.

Keep API DTOs in the consumer for consumer endpoints. The startup list is `{projects: Project[], total: number}`, not `PaginatedResponse<T>`. Generic type parameters do not validate incoming JSON. A product needing stronger boundaries should parse the response explicitly.

## HTTP calls

| Method | Additional arguments |
| --- | --- |
| `get<T>(path, params?, config?)` | Query values string/number/undefined |
| `post<T>`, `put<T>`, `patch<T>` | `body?`, `config?` |
| `delete<T>` | `config?` |
| `upload<T>` | `File`, `fieldName?` (default `file`), `config?` |
| `download` | filename, params?, config?; browser download |

`config` includes `headers`, `signal`, `params`. Pass AbortController signals on reads and cancel stale reads on unmount. Never silently replay failed mutations without an endpoint-level idempotency contract. Functional SDK `defaultHeaders` is evaluated per request; the admin factory does not expose that option. Refresh and SSE have separate transports; do not assume default headers propagate there. The client owns bearer authentication.

JSON responses return decoded JSON, empty successful bodies return undefined, legacy `text/plain` success bodies return strings. New core mutation responses should be JSON or an explicitly empty status. `application/problem+json` errors preserve problem details; non-JSON error responses become `ApiError`. Malformed JSON and unexpected HTML must not masquerade as valid API data. Verify this against the installed SDK version when upgrading old consumers.

Access tokens stay in memory; refresh token/user/expiry persist under the storage key. The SDK uses a session epoch so a late refresh cannot resurrect a logged-out session. Preserve single-flight refresh and cross-app isolation. Do not manually persist access tokens or implement a competing refresh loop in a page.

## User experience contract

For every product page specify initial loading, empty list, populated list, denied access, server/validation failure, saving and successful mutation. Preserve input after failure. Prevent double submission, surface permission-dependent actions and supply accessible labels/status feedback. Abort or ignore stale responses so switching filters/owners cannot paint obsolete data. Render bounded lists/pagination; do not claim a count equals the visible rows when capped.

Use the shared theme/components and ordinary React escaping. Never embed secrets in `VITE_*`; values are public build output. Compile-time configuration changes require rebuilding deployed static assets. Configure SPA fallback to index.html under basePath and verify a deep-link reload.

## External packaging

Within core-ui, Bun workspaces and development Vite aliases resolve package source. Production builds resolve dist. An independent consumer cannot use `workspace:*`, sibling source aliases or the workspace tsconfig inheritance. Replace these with exact released versions or pinned local tarballs and a standalone bundler tsconfig/Vite config. Run `scripts/pack-test.sh` for the actual packaged declarations/JS/CSS; local overrides are not proof of registry resolution.

Read the pinned `packages/*/README.md`, `packages/admin/src/core/module.ts`, factory and `apps/startup/src` when changing composition. See start-product for concrete standalone commands.
