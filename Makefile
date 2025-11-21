TOPDIR=$(PWD)
GH_ORG_LC="robertkielty"
REGISTRY ?= ghcr.io
IMAGE ?= $(REGISTRY)/$(GH_ORG_LC)/maintainerd:latest
WHOAMI=$(shell whoami)

# Helpful context string for logs
CTX_STR := $(if $(KUBECONTEXT),$(KUBECONTEXT),$(shell kubectl config current-context 2>/dev/null || echo current))

# kcp release download settings
KCP_VERSION ?= 0.28.3
KCP_TAG ?= v$(KCP_VERSION)
KCP_OS ?= $(shell uname | tr '[:upper:]' '[:lower:]')
KCP_ARCH ?= $(shell uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
KCP_TAR ?= kcp_$(KCP_VERSION)_$(KCP_OS)_$(KCP_ARCH).tar.gz
APIGEN_TAR ?= apigen_$(KCP_VERSION)_$(KCP_OS)_$(KCP_ARCH).tar.gz
KCP_CHECKSUMS ?= kcp_$(KCP_VERSION)_checksums.txt
KCP_RELEASE_URL ?= https://github.com/kcp-dev/kcp/releases/download/$(KCP_TAG)
BIN_DIR ?= $(TOPDIR)/bin
KCP_BIN := $(BIN_DIR)/kcp
APIGEN_BIN := $(BIN_DIR)/apigen
CONTROLLER_GEN ?= $(BIN_DIR)/controller-gen
APIGEN ?= $(APIGEN_BIN)
GOCACHE_DIR ?= $(TOPDIR)/.gocache
KCP_CRD_DIR ?= $(TOPDIR)/config/crd/bases
KCP_SCHEMA_DIR ?= $(TOPDIR)/config/kcp
KCP_RESOURCES := collaborators companies maintainers onboardingtasks projectmemberships projects services serviceteams serviceusers serviceuserteams

# GHCR auth (optional for push). If set, we will docker login before push.
GHCR_USER  ?= $(DOCKER_REGISTRY_USERNAME)
GHCR_TOKEN ?= $(GITHUB_GHCR_TOKEN)



# ---- Image ----
.PHONY: image
image:
	@echo "Building container image: $(IMAGE)"
	@docker buildx build -t $(IMAGE) -f Dockerfile .
	@echo "Ensuring docker is logged in to $(REGISTRY) (uses GHCR_TOKEN if set)"
	@if [ -n "$(GHCR_TOKEN)" ]; then \
		echo "Logging into $(REGISTRY) as $(GHCR_USER) using token from GHCR_TOKEN"; \
		echo "$(GHCR_TOKEN)" | docker login $(REGISTRY) -u "$(GHCR_USER)" --password-stdin; \
	else \
		echo "GHCR_TOKEN not set; attempting push with existing docker auth"; \
	fi
	@echo "Pushing image: $(IMAGE)"
	@docker push $(IMAGE)
	@echo "Image pushed. Attempting rollout on context $(CTX_STR)."
	@CTX_FLAG="$(if $(KUBECONTEXT),--context $(KUBECONTEXT))" ; \
	if kubectl $$CTX_FLAG config current-context >/dev/null 2>&1; then \
		echo "Updating Deployment/maintainerd image to $(IMAGE) [ctx=$(CTX_STR)]"; \
		kubectl -n $(NAMESPACE) $$CTX_FLAG set image deploy/maintainerd server=$(IMAGE) bootstrap=$(IMAGE); \
		echo "Rolling restart Deployment/maintainerd [ctx=$(CTX_STR)]"; \
		kubectl -n $(NAMESPACE) $$CTX_FLAG rollout restart deploy/maintainerd; \
		echo "Waiting for rollout to complete [ctx=$(CTX_STR)]"; \
		kubectl -n $(NAMESPACE) $$CTX_FLAG rollout status deploy/maintainerd --timeout=180s; \
	else \
		echo "kubectl context $(CTX_STR) unavailable; skipping rollout"; \
	fi

.PHONY: image-push
image-push: image
	@true

.PHONY: image-run
image-run: image
	@docker run -ti --rm $(IMAGE)

# ---- Config ----
NAMESPACE ?= maintainerd
ENVSRC    ?= .envrc
ENVOUT    ?= bootstrap.env
KUBECONTEXT ?=context-cdv2c4jfn5q


# Secret names (keep these stable across clusters)
ENV_SECRET_NAME   ?= maintainerd-bootstrap-env
CREDS_SECRET_NAME ?= workspace-credentials

# Path to the JSON creds file on your machine
CREDS_FILE ?= ./cmd/bootstrap/credentials.json
CREDS_KEY  ?= credentials.json

# Docker registry (for ghcr secret)
DOCKER_REGISTRY_SERVER ?= ghcr.io
DOCKER_REGISTRY_USERNAME ?= robertkielty
DOCKER_REGISTRY_PASSWORD ?= $(GITHUB_GHCR_TOKEN)

# ---- Helpers ----
.PHONY: help
help:
	@echo "== Testing =="
	@echo "make test            -> run all tests"
	@echo "make test-verbose    -> run tests with verbose output"
	@echo "make test-coverage   -> run tests with coverage report"
	@echo "make test-race       -> run tests with race detector"
	@echo "make test-package    -> run tests for specific package (use PKG=...)"
	@echo "make ci-local        -> run all CI checks locally (fmt, vet, staticcheck, test)"
	@echo "make lint            -> run linters (requires golangci-lint)"
	@echo ""
	@echo "== Deployment =="
	@echo "make secrets         -> build $(ENVOUT) from $(ENVSRC) and apply both Secrets"
	@echo "make env             -> build $(ENVOUT) from $(ENVSRC)"
	@echo "make apply-env       -> create/update $(ENV_SECRET_NAME) from $(ENVOUT)"
	@echo "make apply-creds     -> create/update $(CREDS_SECRET_NAME) from $(CREDS_FILE)"
	@echo "make clean-env       -> remove $(ENVOUT)"
	@echo "make print           -> show which keys would be loaded (without values)"
	@echo "make image           -> build+push $(IMAGE), then restart Deployment in $(NAMESPACE)"
	@echo "                      (uses GHCR_TOKEN/GITHUB_GHCR_TOKEN + GHCR_USER/DOCKER_REGISTRY_USERNAME for ghcr login)"
	@echo "make ensure-ns       -> ensure namespace $(NAMESPACE) exists"
	@echo "make apply-ghcr-secret -> create/update docker-registry Secret 'ghcr-secret'"
	@echo "make manifests-apply -> kubectl apply -f deploy/manifests (prod-only)"
	@echo "make manifests-delete-> kubectl delete -f deploy/manifests (cleanup)"
	@echo "make cluster-up      -> ensure ns + secrets + ghcr secret + apply manifests"
	@echo "make maintainerd-delete -> delete Deployment/Service maintainerd"
	@echo "make maintainerd-restart -> rollout restart Deployment/maintainerd"
	@echo "make maintainerd-drain   -> scale Deployment/maintainerd to 0 and wait for pods to exit"
	@echo "make maintainerd-port-forward -> forward :2525 -> svc/maintainerd:2525"
	@echo "make cluster-down    -> delete manifests applied via deploy/manifests"
	@echo "make kcp-install     -> download kcp $(KCP_VERSION) binaries into $(BIN_DIR)"

# Convert .envrc (export FOO=bar) to KEY=VALUE lines
# - drops comments/blank lines
# - strips a leading 'export' and surrounding whitespace
.PHONY: env
env: $(ENVOUT)

$(ENVOUT): $(ENVSRC)
	@echo "Generating $(ENVOUT) from $(ENVSRC)"
	@sed -E '/^[[:space:]]*#/d; /^[[:space:]]*$$/d; s/^[[:space:]]*export[[:space:]]+//' $(ENVSRC) > $(ENVOUT)

# Apply the Secret with all bootstrap env vars
.PHONY: apply-env
apply-env: $(ENVOUT)
	@echo "Applying secret $(ENV_SECRET_NAME) in namespace $(NAMESPACE) [ctx=$(CTX_STR)]"
	@kubectl -n $(NAMESPACE) create secret generic $(ENV_SECRET_NAME) \
		--from-env-file=$(ENVOUT) \
		$(if $(KUBECONTEXT),--context $(KUBECONTEXT)) \
		--dry-run=client -o yaml | kubectl -n $(NAMESPACE) apply -f -

# Apply the Secret that contains the credentials file
.PHONY: apply-creds
apply-creds:
	@[ -f "$(CREDS_FILE)" ] || (echo "Missing $(CREDS_FILE). Set CREDS_FILE=... or place the file."; exit 1)
	@echo "Applying secret $(CREDS_SECRET_NAME) in namespace $(NAMESPACE) [ctx=$(CTX_STR)]"
	@kubectl -n $(NAMESPACE) create secret generic $(CREDS_SECRET_NAME) \
		--from-file=$(CREDS_KEY)=$(CREDS_FILE) \
		$(if $(KUBECONTEXT),--context $(KUBECONTEXT)) \
		--dry-run=client -o yaml | kubectl -n $(NAMESPACE) apply -f -

# Convenience combo target
.PHONY: secrets
secrets: env apply-env apply-creds
	@echo "Secrets applied: $(ENV_SECRET_NAME), $(CREDS_SECRET_NAME) [ns=$(NAMESPACE)]"

# Show which keys would be loaded (without values)
.PHONY: print
print: env
	@echo "Keys in $(ENVOUT):"
	@cut -d= -f1 $(ENVOUT)

.PHONY: clean-env
clean-env:
	@rm -f $(ENVOUT)
	@echo "Removed $(ENVOUT)"

# ---- Cluster helpers ----
.PHONY: ensure-ns
ensure-ns:
	@echo "Ensuring namespace $(NAMESPACE) exists [ctx=$(CTX_STR)]"
	@kubectl $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) get ns $(NAMESPACE) >/dev/null 2>&1 \
		|| kubectl $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) create ns $(NAMESPACE)

.PHONY: apply-ghcr-secret
apply-ghcr-secret:
	@:${DOCKER_REGISTRY_USERNAME:?Set DOCKER_REGISTRY_USERNAME (e.g. your GitHub username)}
	@:${DOCKER_REGISTRY_PASSWORD:?Set DOCKER_REGISTRY_PASSWORD (a PAT with package:read)}
	@echo "Applying docker-registry secret 'ghcr-secret' in namespace $(NAMESPACE) [ctx=$(CTX_STR)]"
	@kubectl -n $(NAMESPACE) create secret docker-registry ghcr-secret \
		--docker-server=$(DOCKER_REGISTRY_SERVER) \
		--docker-username=$(DOCKER_REGISTRY_USERNAME) \
		--docker-password=$(DOCKER_REGISTRY_PASSWORD) \
		$(if $(KUBECONTEXT),--context $(KUBECONTEXT)) \
		--dry-run=client -o yaml | kubectl -n $(NAMESPACE) apply -f -


# ---- Plain manifests (no Helm/Argo CD) ----
.PHONY: manifests-apply
manifests-apply:
	@echo "Applying manifests in deploy/manifests to namespace $(NAMESPACE) [ctx=$(CTX_STR)]"
	@kubectl -n $(NAMESPACE) $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) apply -f deploy/manifests

.PHONY: manifests-delete
manifests-delete:
	@echo "Deleting manifests in deploy/manifests from namespace $(NAMESPACE) [ctx=$(CTX_STR)]"
	@kubectl -n $(NAMESPACE) $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) delete -f deploy/manifests --ignore-not-found
.PHONY: cluster-up
cluster-up: ensure-ns apply-ghcr-secret secrets manifests-apply
	@echo "Maintainerd resources applied to $(NAMESPACE) [ctx=$(CTX_STR)]"

.PHONY: cluster-down
cluster-down: manifests-delete
	@echo "Maintainerd manifests removed from $(NAMESPACE) [ctx=$(CTX_STR)]"



.PHONY: maintainerd-delete
maintainerd-delete:
	@echo "Deleting Deployment/Service 'maintainerd' from $(NAMESPACE) [ctx=$(CTX_STR)]"
	@kubectl -n $(NAMESPACE) $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) delete deploy/maintainerd svc/maintainerd --ignore-not-found

.PHONY: maintainerd-restart
maintainerd-restart:
	@echo "Rolling out restart for Deployment/maintainerd [ctx=$(CTX_STR)]"
	@kubectl -n $(NAMESPACE) $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) rollout restart deploy/maintainerd

.PHONY: maintainerd-drain
maintainerd-drain:
	@echo "Scaling Deployment/maintainerd to 0 replicas [ctx=$(CTX_STR)]"
	@kubectl -n $(NAMESPACE) $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) scale deploy/maintainerd --replicas=0
	@echo "Waiting for maintainerd pods to terminate [ctx=$(CTX_STR)]"
	@kubectl -n $(NAMESPACE) $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) wait --for=delete pod -l app=maintainerd --timeout=120s 2>/dev/null || \
		echo "No maintainerd pods left to delete"

.PHONY: maintainerd-port-forward
maintainerd-port-forward:
	@echo "Port-forwarding localhost:2525 -> service/maintainerd:2525 [ctx=$(CTX_STR)]"
	@kubectl -n $(NAMESPACE) $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) port-forward svc/maintainerd 2525:2525

.PHONY: kcp-install
kcp-install:
	@mkdir -p $(BIN_DIR)
	@echo "Fetching kcp $(KCP_VERSION) for $(KCP_OS)/$(KCP_ARCH)"
	@TMP_DIR=$$(mktemp -d); \
	set -euo pipefail; \
	echo "+ curl -sSL $(KCP_RELEASE_URL)/$(KCP_CHECKSUMS)"; \
	curl -sSL -o $$TMP_DIR/$(KCP_CHECKSUMS) $(KCP_RELEASE_URL)/$(KCP_CHECKSUMS); \
	for tarball in $(KCP_TAR) $(APIGEN_TAR); do \
		echo "+ curl -sSL $(KCP_RELEASE_URL)/$$tarball -o $$TMP_DIR/$$tarball"; \
		curl -sSL -o $$TMP_DIR/$$tarball $(KCP_RELEASE_URL)/$$tarball; \
		grep " $$tarball$$" $$TMP_DIR/$(KCP_CHECKSUMS) > $$TMP_DIR/$$tarball.sha256; \
		SUM=$$(cut -d' ' -f1 $$TMP_DIR/$$tarball.sha256); \
		( cd $$TMP_DIR && sha256sum --check $$tarball.sha256 ); \
		echo "Verified $$tarball (sha256=$$SUM)"; \
	done; \
	tar -xzf $$TMP_DIR/$(KCP_TAR) -C $(BIN_DIR) --strip-components=1 bin/kcp; \
	tar -xzf $$TMP_DIR/$(APIGEN_TAR) -C $(BIN_DIR) --strip-components=1 bin/apigen; \
	chmod +x $(KCP_BIN) $(APIGEN_BIN); \
	rm -rf $$TMP_DIR; \
	echo "Installed kcp and apigen into $(BIN_DIR)"

.PHONY: kcp-generate
kcp-generate:
	@[ -x "$(CONTROLLER_GEN)" ] || { echo "Missing controller-gen binary at $(CONTROLLER_GEN). Install it or set CONTROLLER_GEN to the binary path."; exit 1; }
	@[ -x "$(APIGEN)" ] || { echo "Missing apigen binary at $(APIGEN). Run 'make kcp-install' or download it manually."; exit 1; }
	@mkdir -p $(GOCACHE_DIR) $(KCP_CRD_DIR) $(KCP_SCHEMA_DIR)
	@echo "Generating CustomResourceDefinitions in $(KCP_CRD_DIR)"
	@GOCACHE=$(GOCACHE_DIR) $(CONTROLLER_GEN) crd paths=./apis/... output:crd:dir=$(KCP_CRD_DIR)
	@rm -f $(KCP_CRD_DIR)/_.yaml
	@TMP_DIR=$$(mktemp -d); \
		set -euo pipefail; \
		echo "Rendering APIResourceSchemas with apigen"; \
		$(APIGEN) --input-dir $(KCP_CRD_DIR) --output-dir $$TMP_DIR; \
		for resource in $(KCP_RESOURCES); do \
			cp $$TMP_DIR/apiresourceschema-$$resource.maintainer-d.cncf.io.yaml $(KCP_SCHEMA_DIR)/schema-$$resource.yaml; \
		done; \
		cp $$TMP_DIR/apiexport-maintainer-d.cncf.io.yaml $(KCP_SCHEMA_DIR)/api-export.yaml; \
		rm -rf $$TMP_DIR; \
		echo "Updated APIExport and APIResourceSchemas in $(KCP_SCHEMA_DIR)"

# ---- Testing and CI ----
.PHONY: test
test:
	@echo "Running tests..."
	@go test ./...

.PHONY: test-verbose
test-verbose:
	@echo "Running tests with verbose output..."
	@go test -v ./...

.PHONY: test-coverage
test-coverage:
	@echo "Running tests with coverage..."
	@go test -coverprofile=coverage.out -covermode=atomic ./...
	@go tool cover -func=coverage.out | tail -n 1
	@echo "To view HTML coverage report: go tool cover -html=coverage.out"

.PHONY: test-race
test-race:
	@echo "Running tests with race detector..."
	@go test -race ./...

.PHONY: test-package
test-package:
	@if [ -z "$(PKG)" ]; then \
		echo "Usage: make test-package PKG=<package>"; \
		echo "Example: make test-package PKG=onboarding"; \
		exit 1; \
	fi
	@echo "Running tests for package: $(PKG)"
	@go test -v ./$(PKG)/...

.PHONY: ci-local
ci-local:
	@echo "Running local CI checks..."
	@echo "→ Verifying dependencies..."
	@go mod verify
	@echo "→ Running go fmt..."
	@if [ "$$(gofmt -s -l . | wc -l)" -gt 0 ]; then \
		echo "❌ Code needs formatting. Run: go fmt ./..."; \
		gofmt -s -l .; \
		exit 1; \
	fi
	@echo "→ Running go vet..."
	@go vet ./...
	@echo "→ Running staticcheck..."
	@if command -v staticcheck >/dev/null 2>&1; then \
		staticcheck ./...; \
	else \
		echo "⚠️  staticcheck not installed. Run: go install honnef.co/go/tools/cmd/staticcheck@latest"; \
	fi
	@echo "→ Running golangci-lint..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "⚠️  golangci-lint not installed. Run: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi
	@echo "→ Running tests with race detector..."
	@go test -race -coverprofile=coverage.out -covermode=atomic ./...
	@echo "→ Coverage report:"
	@go tool cover -func=coverage.out | tail -n 1
	@echo "✅ All CI checks passed!"

.PHONY: lint
lint:
	@echo "Running golangci-lint..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed."; \
		echo "Install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		exit 1; \
	fi

.PHONY: fmt
fmt:
	@echo "Formatting code..."
	@go fmt ./...

.PHONY: vet
vet:
	@echo "Running go vet..."
	@go vet ./...
