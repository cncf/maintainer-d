TOPDIR=$(PWD)
GH_ORG_LC="robertkielty"
REGISTRY ?= ghcr.io
IMAGE ?= $(REGISTRY)/$(GH_ORG_LC)/maintainerd:latest
WHOAMI=$(shell whoami)

# Helpful context string for logs
CTX_STR := $(if $(KUBECONTEXT),$(KUBECONTEXT),$(shell kubectl config current-context 2>/dev/null || echo current))

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
	@echo "make secrets         -> build $(ENVOUT) from $(ENVSRC) and apply both Secrets"
	@echo "make env             -> build $(ENVOUT) from $(ENVSRC)"
	@echo "make apply-env       -> create/update $(ENV_SECRET_NAME) from $(ENVOUT)"
	@echo "make apply-creds     -> create/update $(CREDS_SECRET_NAME) from $(CREDS_FILE)"
	@echo "make clean-env       -> remove $(ENVOUT)"
	@echo "make print           -> show which keys would be loaded (without values)"
	@echo "make kind-up         -> create kind cluster 'maintainerd'"
	@echo "make kind-down       -> delete kind cluster 'maintainerd'"
	@echo "make image           -> build+push $(IMAGE), then restart Deployment if kind up"
	@echo "                      (uses GHCR_TOKEN/GITHUB_GHCR_TOKEN + GHCR_USER/DOCKER_REGISTRY_USERNAME for ghcr login)"
	@echo "make ensure-ns       -> ensure namespace $(NAMESPACE) exists"
	@echo "make apply-ghcr-secret -> create/update docker-registry Secret 'ghcr-secret'"
	@echo "make manifests-apply -> kubectl apply -f deploy/manifests (prod-only)"
	@echo "make manifests-delete-> kubectl delete -f deploy/manifests (cleanup)"
	@echo "make cluster-up     -> kind-up + ns + ghcr secret + secrets + manifests-apply"
	@echo "make maintainerd-delete -> delete Deployment/Service maintainerd"
	@echo "make maintainerd-restart -> rollout restart Deployment/maintainerd"
	@echo "make maintainerd-drain   -> scale Deployment/maintainerd to 0 and wait for pods to exit"
	@echo "make maintainerd-port-forward -> forward :2525 -> svc/maintainerd:2525"
	@echo "make cluster-down    -> tear down kind cluster 'maintainerd'"
	@echo "make kind-load-image -> build+load local image into kind and patch deploy to use it [ctx=$(CTX_STR)]"

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

# ---- kind cluster lifecycle ----
.PHONY: kind-up
kind-up:
	@echo "Ensuring kind cluster 'maintainerd' exists [ctx=$(CTX_STR)]"
	@kind get clusters | grep -qx maintainerd || kind create cluster --name maintainerd --config hack/kind-config.yaml
	@echo "Kube context set to $(KUBECONTEXT)"

.PHONY: kind-down
kind-down:
	@echo "Deleting kind cluster 'maintainerd'"
	@kind delete cluster --name maintainerd || true

## ---- Cluster helpers ----
.PHONY: ensure-ns
ensure-ns:
	@echo "Ensuring namespace $(NAMESPACE) exists [ctx=$(CTX_STR)]"
	@kubectl $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) get ns $(NAMESPACE) >/dev/null 2>&1 \
		|| kubectl create ns $(NAMESPACE)

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


 
# ---- ingress controller (nginx) for kind ----
.PHONY: ingress-nginx-install
ingress-nginx-install:
	@echo "Installing ingress-nginx controller (kind provider) [ctx=$(CTX_STR)]"
	@kubectl $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml
	@echo "Waiting for ingress controller to be Ready [ctx=$(CTX_STR)]"
	@kubectl $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) -n ingress-nginx wait --for=condition=Ready pod -l app.kubernetes.io/component=controller --timeout=180s

# High-level: bring up full local stack for bootstrap (plain manifests)
.PHONY: cluster-up
cluster-up: kind-up ensure-ns apply-ghcr-secret secrets manifests-apply
	@echo "Cluster ready. Manifests applied."

.PHONY: cluster-down
cluster-down: kind-down
	@echo "Cluster 'maintainerd' deleted"



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

# ---- local dev: build and use local image in kind ----
.PHONY: kind-load-image
kind-load-image:
	@echo "Building local image: $(IMAGE)"
	@docker build -t $(IMAGE) -f Dockerfile .
	@echo "Loading image into kind cluster 'maintainerd'"
	@kind load docker-image $(IMAGE) --name maintainerd
	@echo "Pointing Deployment at $(IMAGE) [ctx=$(CTX_STR)]"
	@kubectl -n $(NAMESPACE) $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) set image deploy/maintainerd server=$(IMAGE) bootstrap=$(IMAGE)
	@echo "Setting imagePullPolicy=IfNotPresent for server and bootstrap [ctx=$(CTX_STR)]"
	@kubectl -n $(NAMESPACE) $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) patch deploy/maintainerd --type=json -p='[{"op":"replace","path":"/spec/template/spec/containers/0/imagePullPolicy","value":"IfNotPresent"},{"op":"replace","path":"/spec/template/spec/initContainers/0/imagePullPolicy","value":"IfNotPresent"}]'
	@echo "Waiting for rollout [ctx=$(CTX_STR)]"
	@kubectl -n $(NAMESPACE) $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) rollout status deploy/maintainerd --timeout=180s
