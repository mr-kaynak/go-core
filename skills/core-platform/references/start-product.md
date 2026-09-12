# Start a product from an empty workspace

## Resolve source and pins

Clone only the repositories needed by the request into a development directory outside the product repository (or an ignored path). Backend-only work requires go-core alone:

```bash
git clone https://github.com/mr-kaynak/go-core.git
# Optional: only for shared SDK, UI or admin work.
git clone https://github.com/mr-kaynak/core-ui.git
```

Read the selected repositories' AGENTS.md, consumer guides and toolchain manifests. Record `git rev-parse HEAD` in each. Check out reviewed refs before copying templates; do not interpret main or a manifest version as a tested release set. Go toolchain is declared in go.mod; Bun is needed only for core-ui package/app work. If a private repo needs authentication, use the user's configured Git credentials without copying tokens into files.

## Get a visible result

From go-core root:

```bash
bash examples/startup/setup.sh
docker compose -f examples/startup/compose.yaml up -d --wait
cd examples/startup
go run .
```

For a backend-only task, verify `/livez`, `/readyz` and the authenticated product endpoints through HTTP; continue with the backend consumer section. No frontend build is required.

Optionally, to demonstrate the paired admin, in a second terminal from core-ui root:

```bash
bun install --frozen-lockfile
bun run build:packages
bun run dev:startup
```

Open `http://localhost:5174`; API is port 3300. Bootstrap creates `admin@system.local` and prints a random initial password only on first creation. Keep it private, change it after login. Create a project and restart the API to prove persistence. `.env` belongs in the startup API directory; startup UI overrides belong in `apps/startup/.env`, not the playground's root environment file.

Local infrastructure: PostgreSQL 55435, Redis 56380, RabbitMQ 55673, SMTP 51026, mail UI 58026; metrics 9300. Docker mappings bind loopback. The API example uses ordinary Fiber listeners; do not expose it as a production service with development credentials. `setup.sh` generates secrets and preserves existing `.env`. Ports may require matching changes to Compose/env/CORS. Stop this stack using its exact Compose path without removing its data volume.

## Create the actual backend consumer

Copy only `examples/startup` source/config template into the new backend: main.go, projects/, setup.sh, compose.yaml, .env.example. Exclude `.env`, uploads, build artifacts and local data. Initialize your own module and change the local projects import:

```bash
# In the new backend directory:
go mod init example.com/acme/backend
# Set GOCORE_REF to the reviewed full commit SHA or verified release tag.
go get "github.com/mr-kaynak/go-core@${GOCORE_REF:?set a reviewed ref}"
```

Replace `github.com/mr-kaynak/go-core/examples/startup/projects` with `example.com/acme/backend/projects` in main.go. Keep `go-core/app` and `go-core/identity`. Run `go mod tidy`, `go test ./...`, then the local setup/run commands. To develop against a checkout use a temporary `go mod edit -replace=github.com/mr-kaynak/go-core=/absolute/path/to/go-core`; remove before distribution with `go mod edit -dropreplace=github.com/mr-kaynak/go-core` and resolve the pinned ref.

Name the product, issuer, exchange/queue prefix, database and metrics/API ports. Each product needs its own compose project name and volume/credentials: edit the copied `name: core-startup-example` before starting a second product, otherwise both copies address the same Compose stack. New products must not share session keys, DBs or broker namespaces accidentally.

Replace projects with the actual domain using backend.md. Keep migration sources identical between normal startup and `-migrate`. For a new source rename before first deployment; for an existing deployed source never rename its identity as part of rebranding.

## Create the actual frontend consumer (optional)

Skip this section for backend-only work or an existing custom frontend. Such clients consume the HTTP contract directly; using core-ui packages is never required by go-core. The SDK currently assumes browser APIs/storage and is not a drop-in native mobile or server-side authentication library.

Copy `core-ui/apps/startup` without node_modules/dist/.env. Change name, branding, `storageKey`, API URL and modules. Replace `workspace:*` with exact versions of SDK/UI/admin from a verified compatible release set. No published set? Use local tarballs from the reviewed core-ui checkout rather than inventing an npm version:

1. Run `bun run build:packages` in core-ui.
2. For each sdk/ui/admin directory, run `bun pm pack --destination /absolute/path/to/product/vendor`. Keep the artifact hashes/ref as evidence.
3. Set the consumer's three dependencies to their actual relative `file:vendor/...tgz` paths. For prepublication local use, set `overrides` for SDK/UI to the same tarballs so admin's transitive dependencies resolve. Do not mistake these overrides for a registry test.
4. Keep React/react-dom and the TypeScript/Vite/React-plugin dev dependencies from the app template. Remove unused Tailwind tooling if using prebuilt CSS.

Use a standalone tsconfig, not a file extending workspace roots:

```json
{
  "compilerOptions": {
    "target": "ES2022", "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "module": "ESNext", "moduleResolution": "bundler", "jsx": "react-jsx",
    "strict": true, "noEmit": true, "skipLibCheck": true,
    "verbatimModuleSyntax": true, "types": ["vite/client"]
  },
  "include": ["src"]
}
```

Use `defineConfig({plugins:[react()]})` from `vite` and `@vitejs/plugin-react` as the Vite config; no workspace source aliases. Run `bun install`, `bun run build`, `bun run dev`. Build output in the standalone consumer is `dist/`; within the source workspace it's `apps/startup/dist/`.

## Make the first domain real

Write the owner/tenant contract before schema. Add SQL, permissions, middleware and owner-filtered handlers. Define JSON DTOs and event payloads; add an admin module only if selected. Prove 401/403, cross-owner 404, creation, input bounds, rollback and restart persistence. Commit dependency lockfiles and a short compatibility record. Do not copy core internal code to solve an absent public feature; make that gap explicit and choose an owned integration or public extension.
