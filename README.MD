# maintainerd
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2FRobertKielty%2Fmaintainerd.svg?type=shield)](https://app.fossa.com/projects/git%2Bgithub.com%2FRobertKielty%2Fmaintainerd?ref=badge_shield)


maintainerd is a database-backed application that tracks Maintainers that work on CNCF Projects using multiple datasources.

At present, a Project Maintainer registers with the CNCF by
 - sending an email triggering a manual add to an internal Google Worksheet
 - requesting access to services via a Service Desk Request

## Proposed Maintainer and Project registration methods
 - maintainer.yaml formally lists out the maintainers for a project
 - openprofile.dev
 - admin front end for CNCF Project Team

# Components
## Database
The maintainerd backend is a database implemented using GORM, a golang object relational mapping tool.

Building and running the resultant db executable loads data from the internal worksheet.

```
cd db
go build
./db
2025/06/10 05:06:34 maintainerd: backend: database successfully seeded demo.db (SQLite)
```
Data is stored in a file called demo.db
MD_WORKSHEET needs to contain the ID of the Goolge Worksheet being read
credentials.json needs to contain the Google Service Account that is allowed to read the
worksheet.

## Service Plugins

A plugin will reconcile the list of maintainers for a project and ensure that they are registered
with their chosen services.

## Make Targets (deploy)

We deploy using plain manifests (no Helm). Key targets:

- env: Generate `bootstrap.env` from `.envrc` (KEY=VALUE lines)
- apply-env: Create/update Secret `maintainerd-bootstrap-env` from `bootstrap.env`
- apply-creds: Create/update Secret `workspace-credentials` from `cmd/bootstrap/credentials.json`
- secrets: Run `env`, `apply-env`, and `apply-creds`
- manifests-apply: `kubectl apply -f deploy/manifests/` into the `maintainerd` namespace
- manifests-delete: Cleanup resources created by the manifests
- maintainerd-port-forward: Forward `localhost:2525 -> svc/maintainerd:2525`

## Manual Deployment

- Ensure namespace and secrets exist:
  - `make ensure-ns`
  - `make apply-ghcr-secret`
  - `make secrets`
- Apply app resources:
  - `make manifests-apply`
- Verify:
  - `kubectl -n maintainerd get pvc,deploy,svc,ing`
  - The Deployment runs an initContainer that bootstraps the DB before the server starts.

## Manual Testing (alternate org/repo)

Use environment variables `ORG` and `REPO` to point the server at a different GitHub org/repo for testing (e.g., your fork). The Deployment passes `-org=$ORG` and `-repo=$REPO` to the server, which resolves those from the environment injected via the bootstrap Secret.

- Set env in `.envrc` (example):

  - `export ORG=robertkielty`
  - `export REPO=maintainerd`

- Regenerate and apply the bootstrap Secret (injects env into the Pod):

  - `make env`
  - `make apply-env`

- Apply/roll the Deployment to pick up changes (if already applied, a rollout is enough):

  - `make manifests-apply`  (or)
  - `kubectl -n maintainerd rollout restart deploy/maintainerd && \\
     kubectl -n maintainerd rollout status deploy/maintainerd --timeout=180s`

- Configure the GitHub webhook on the alternate repo:

  - Payload URL: `http://maintainerd.localtest.me/webhook`
  - Content type: `application/json`
  - Secret: must match `GITHUB_WEBHOOK_SECRET` in `maintainerd-bootstrap-env`
  - Events: at least “Issues” and “Issue comments”

- Exercise the flow on the test repo:

  - Create or use an onboarding issue titled `"[PROJECT ONBOARDING] <project>"`
  - Add label `fossa` (creates the FOSSA team via the label flow)
  - Assign yourself to the issue (assignees can trigger commands)
  - Comment exactly: `/fossa-invite accepted`
  - Watch logs: `kubectl -n maintainerd logs deploy/maintainerd -f`

- Notes:

  - Public comments never include email addresses; only GitHub handles are shown.
  - If the team does not exist, the server comments instructions to add the `fossa` label first and retry.


## License
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2FRobertKielty%2Fmaintainerd.svg?type=large)](https://app.fossa.com/projects/git%2Bgithub.com%2FRobertKielty%2Fmaintainerd?ref=badge_large)