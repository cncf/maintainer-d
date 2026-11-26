# AGENTS Guidelines for this Repo
This document gives agents instructions and tips for working in this repository. It applies to the entire repo.

## Where the Code Lives — Read This First

**STOP. The directory you are running in is not necessarily the code.** A project holding only
`.spektacular/`, `knowledge/` and `repos/` is normal: its code lives wherever the registry says.

- **Before any code-touching work** (searching, reading, editing, running tests, verifying, or
  answering how something works), run `spektacular repo list` and work in each repo's `root`.
- **Never assume the starting directory holds a repo's code, and never guess a path.** The
  registry is the only authority on where code lives.
- **Pass the relevant repo's `root` explicitly to every sub-agent you launch.**
- **If a repo's `root` is not on disk, stop and tell the user** the path the registry gave.

This binds everywhere: every workflow, ad-hoc question, skill and sub-agent.
Managed by `spektacular init`; full guidance: [.spektacular/guidance/repo-sources.md](.spektacular/guidance/repo-sources.md).

## Purpose & Overview
- `maintainer-d` is a Go service with a Postgres-backed data layer that processes GitHub webhooks and onboards projects to services (e.g., FOSSA).
- Deployment uses plain Kubernetes manifests under `deploy/manifests` (production only).

## Database Constraint
- Treat this repository as Postgres-only for production changes and new operational code.
- Do not add new SQLite-specific behavior, fallbacks, or documentation for new work unless the user explicitly asks for it.

## Deploy Rules
- Apply manifests in `deploy/manifests` with `kubectl -n maintainerd apply -k deploy/manifests` (`make manifests-apply`). That directory holds only long-lived resources (Deployments, Services, Ingresses, CronJobs, cert-manager objects, the ServiceAccount) and is safe to re-apply.
- Production image tags are pinned in `deploy/manifests/kustomization.yaml`. Deploy a new tag with `make image-set TAG=<tag>` (or a per-service `*-image-set`), then commit that file. Never `kubectl set image` or apply single manifests with `-f`; both bypass the pinned tags.
- One-shot Jobs (schema migration, backfills, add-staff, profile sync) live in `deploy/jobs/`. Apply them individually and on purpose; never apply that directory wholesale.
- Ensure the `ghcr-secret` Secret and the per-workload env Secrets exist beforehand.

## Makefile Targets (high‑value)
- `secrets` → dev-only: builds `bootstrap.env` and applies `maintainerd-bootstrap-env`.
- `manifests-apply` → apply all manifests to the cluster.
- `manifests-delete` → delete the manifests from the cluster.

## Required Secrets & Config
- Secrets (namespace `maintainerd`):
  - `maintainerd-bootstrap-env` (from `bootstrap.env`)
  - `ghcr-secret` (Docker registry creds for `ghcr.io`)
- Env vars used by the app:
  - `FOSSA_API_TOKEN`
  -  GitHub tokens/secrets for the server: `GITHUB_API_TOKEN`, `GITHUB_WEBHOOK_SECRET`.

## Local Validation & Testing
- Watch resources after apply: `kubectl -n maintainerd get deploy,job,svc,ing,pvc -w`.
- If DB schema/config changes (e.g., extensions, search functions), update and verify local DB scripts in `scripts/` (refresh/restore, init).

## Notes
- Verify Secrets exist and the image tag is pullable from the GitHub Container Registry.
- If DB configuration changes (extensions, schema requirements, etc.), update the local DB scripts in `scripts/` (e.g., `refresh-local-podman-db.sh`, `pg_restore_init.sh`) to keep dev workflows aligned.
- FOSSA rule: `remote_teams.remote_team_id` must be sourced from the FOSSA API. Never derive it from `project.id` or any local identifier.

## Coding Guidelines
- Keep changes minimal and focused. Match existing Go style and module layout.
- Avoid unrelated refactors in the same change. Update docs/Makefile if deployment surface changes.
- Use the "The Friends method" of writing commit messages to describe patches where each commit message is notionally pre-fixed with the sentence, "This is the change that " and then the commit message continues this sentence, for example, (this is the change that) "adds the repo being monitored to log output at loglevel INFO"

## Web UI Theme Rule
- When changing the web UI, always make the change work in both light mode and dark mode.
- Respect the current `ThemeProvider` / `data-theme` mechanism instead of assuming light mode.
- Prefer existing CSS variables in `web/src/app/globals.css`. When new UI colors or surfaces are needed, add theme-aware variables there and use those variables in component CSS.
- Do not ship a web UI change that only looks correct in light mode.

## Spektacular's Files Are Reached Through Spektacular

Never read, list, write, edit or delete a store file (specs, plans, changelog, knowledge, designs,
epics) with your own tools, and never build a store path by hand: use the `spektacular` CLI, writing
with `--from <path>`. Yours to write: `.spektacular/tmp/`, `.spektacular/work/<name>/`,
`.spektacular/working-context.md`; yours to read, never edit: `.spektacular/guidance/`. This binds
every sub-agent: say so when delegating. Before touching `.spektacular/`, read the full guidance.
Managed by `spektacular init`; full guidance: [.spektacular/guidance/store-access.md](.spektacular/guidance/store-access.md).

## Memory & Context

In this repository, never write to your per-user memory store; route anything worth remembering
through the `spek-knowledge` skill instead. Outside this repository, use your memory as normal.
Managed by `spektacular init`; full guidance: [.spektacular/guidance/memory-context.md](.spektacular/guidance/memory-context.md).

## Knowledge-Worthy Discovery Recognition

When you surface something durable and non-obvious (an unwritten convention, a gotcha, the reason
behind a choice), write it via `spek-knowledge` yourself, then tell the user which entry, where
(tier, store, path) and why. Do not ask first. Before writing, read the full guidance.
Managed by `spektacular init`; full guidance: [.spektacular/guidance/knowledge-trigger.md](.spektacular/guidance/knowledge-trigger.md).

## Spec-Worthy Discussion Recognition

When a discussion or a request (an issue, a ticket, a pasted doc) reaches spec-worthy detail,
offer to capture it as a spec before implementing; never start the spec workflow unilaterally.
Calibrate with `spec_trigger_threshold` in `.spektacular/config.yaml`, read at decision time.
Before offering, read the full guidance.
Managed by `spektacular init`; full guidance: [.spektacular/guidance/spec-trigger.md](.spektacular/guidance/spec-trigger.md).

## Design-Worthy Detail Recognition

When a discussion settles a worked design (an API shape, a flow, a data format) too large to
write into a spec, or the user brings or changes a design document, offer to capture it with the
`spek-design` skill; never write a design document unprompted. Before offering, read the full
guidance.
Managed by `spektacular init`; full guidance: [.spektacular/guidance/design-trigger.md](.spektacular/guidance/design-trigger.md).

## Presenting Drafts and Confirmations

Always show a draft in full as plain chat text first; never put the draft itself inside a yes/no
or multiple-choice dialog. A short confirmation question may follow it.
Managed by `spektacular init`; full guidance: [.spektacular/guidance/draft-presentation.md](.spektacular/guidance/draft-presentation.md).

## Historical Artifacts: Specs and Plans as Archaeology

Specs and plans under `.spektacular/specs/` and `.spektacular/plans/` record past intent, not
current behaviour: never read them to explain how the code works today, only for "why was X
built this way?" questions or within the workflow that owns them. Knowledge entries are binding.
Before reading a spec or plan, read the full guidance.
Managed by `spektacular init`; full guidance: [.spektacular/guidance/historical-artifacts.md](.spektacular/guidance/historical-artifacts.md).
