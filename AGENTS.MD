# AGENTS Guidelines for this Repo
This document gives agents instructions and tips for working in this repository. It applies to the entire repo.

## Purpose & Overview
- `maintainer-d` is a Go service with a SQLite-backed data layer that processes GitHub webhooks and onboards projects to services (e.g., FOSSA).
- Deployment uses plain Kubernetes manifests under `deploy/manifests` (production only).

## Deploy Rules
- Apply manifests in `deploy/manifests` with `kubectl -n maintainerd apply -f deploy/manifests`.
- Ensure `ghcr-secret`, `maintainerd-bootstrap-env`, and `workspace-credentials` Secrets exist beforehand.

## Makefile Targets (high‑value)
- `secrets` → builds `bootstrap.env` and applies required Secrets.
- `manifests-apply` → apply all manifests to the cluster.
- `manifests-delete` → delete the manifests from the cluster.

## Required Secrets & Config
- Secrets (namespace `maintainerd`):
  - `maintainerd-bootstrap-env` (from `bootstrap.env`)
  - `workspace-credentials` (key: `credentials.json`)
  - `ghcr-secret` (Docker registry creds for `ghcr.io`)
- Env vars used by bootstrap/app (typically in `bootstrap.env`):
  - `FOSSA_API_TOKEN`, `MD_WORKSHEET`, `WORKSPACE_CREDENTIALS_FILE` (the Job sets this to `/creds/credentials.json`), 
  -  GitHub tokens/secrets for the server: `GITHUB_API_TOKEN`, `GITHUB_WEBHOOK_SECRET`.

## Local Validation & Testing
- Watch resources after apply: `kubectl -n maintainerd get deploy,job,svc,ing,pvc -w`.

## Notes
- PVC will bind when the Deployment mounts it. The Deployment includes an initContainer that bootstraps the DB before the server starts.
- Verify Secrets exist and the image tag is pullable from the GitHub Container Registry.

## Coding Guidelines
- Keep changes minimal and focused. Match existing Go style and module layout.
- Avoid unrelated refactors in the same change. Update docs/Makefile if deployment surface changes.
- Use the "The Friends method" of writing commit messages to describe patches where each commit message is notionally pre-fixed with the sentence, "This is the change that " and then the commit message continues this sentence, for example, (this is the change that) "adds the repo being monitored to log output at loglevel INFO"

## CRD Workflow (Generated)
- CRDs and deepcopy files are generated; do not hand-edit `config/crd/bases/*.yaml`, `config/kcp/*.yaml`, or `apis/maintainers/v1alpha1/zz_generated.deepcopy.go`.
- When changing existing CRDs: update Go types and kubebuilder markers in `apis/maintainers/v1alpha1/types.go`, then regenerate.
- When adding new CRDs: add the new type + kubebuilder markers in `apis/maintainers/v1alpha1/types.go`, include it in `addKnownTypes`, then regenerate.
- Regeneration command: `make kcp-generate` (requires `controller-gen` and `apigen`; run `make kcp-install` if needed).
- CRDs are generated from `apis/maintainers/v1alpha1/types.go`, not from `model/main.go`. DB model changes do not automatically reflect in CRDs; update API types explicitly when you want schema changes exposed.

## Model → CRD Exposure Checklist
- Decide whether the DB change should be exposed via CRD or remain internal.
- If exposed, update `apis/maintainers/v1alpha1/types.go` (fields + kubebuilder markers). Add to `addKnownTypes` for new resources.
- Update the sync/mapping layer to populate the new fields (e.g., `cmd/sync/main.go` or store accessors).
- Regenerate artifacts: `make kcp-generate`.
- Review generated diffs under `config/crd/bases/` and `config/kcp/`.
- Run CI (at least `make ci-local` or lint + tests).
f
