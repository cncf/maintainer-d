TOPDIR=$(PWD)
GH_ORG_LC="robertkielty"
REGISTRY ?= ghcr.io
IMAGE ?= $(REGISTRY)/$(GH_ORG_LC)/maintainerd:latest
WHOAMI=$(shell whoami)



# ---- Image ----
.PHONY: image
image:
	@echo "Building container image: $(IMAGE)"
	@docker buildx build -t $(IMAGE) -f Dockerfile .
	@echo "Pushing image: $(IMAGE)"
	@docker push $(IMAGE)
	@echo "Image pushed. If kind cluster 'maintainerd' is running, rolling restart Deployment."
	@kind get clusters | grep -qx maintainerd \
		&& kubectl -n $(NAMESPACE) $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) rollout restart deploy/maintainerd \
		&& kubectl -n $(NAMESPACE) $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) rollout status deploy/maintainerd --timeout=180s \
		|| echo "kind 'maintainerd' not running; skip k8s rollout"

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
	@echo "make kind-up         -> create kind cluster 'maintainerd' and install Argo CD"
	@echo "make kind-down       -> delete kind cluster 'maintainerd'"
	@echo "make image           -> build+push $(IMAGE), then restart Deployment if kind up"
	@echo "make argocd-install  -> install Argo CD into the cluster"
	@echo "make argo-bootstrap  -> apply Argo CD project and applications (deploy/argocd)"
	@echo "make argocd-ui       -> port-forward Argo CD UI to https://localhost:8088"
	@echo "make ensure-ns       -> ensure namespace $(NAMESPACE) exists"
	@echo "make apply-ghcr-secret -> create/update docker-registry Secret 'ghcr-secret'"
	@echo "make apply-pvc       -> apply pvc.yaml into ns $(NAMESPACE)"
	@echo "make bootstrap-apply -> apply bootstrap.yaml Job into ns $(NAMESPACE)"
	@echo "make bootstrap-wait  -> wait for bootstrap Job completion"
	@echo "make bootstrap-restart-> delete and re-run bootstrap Job"
	@echo "make cluster-up      -> kind-up + ns + ghcr secret + secrets + pvc + bootstrap"
	@echo "make maintainerd-apply -> deploy maintainerd.yaml (Deployment + Service + Ingress)"
	@echo "make maintainerd-delete -> delete Deployment/Service maintainerd"
	@echo "make maintainerd-restart -> rollout restart Deployment/maintainerd"
	@echo "make maintainerd-port-forward -> forward :2525 -> svc/maintainerd:2525"
	@echo "make cluster-down    -> tear down kind cluster 'maintainerd'"

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
	@echo "Applying secret $(ENV_SECRET_NAME) in namespace $(NAMESPACE)"
	@kubectl -n $(NAMESPACE) create secret generic $(ENV_SECRET_NAME) \
		--from-env-file=$(ENVOUT) \
		$(if $(KUBECONTEXT),--context $(KUBECONTEXT)) \
		--dry-run=client -o yaml | kubectl -n $(NAMESPACE) apply -f -

# Apply the Secret that contains the credentials file
.PHONY: apply-creds
apply-creds:
	@[ -f "$(CREDS_FILE)" ] || (echo "Missing $(CREDS_FILE). Set CREDS_FILE=... or place the file."; exit 1)
	@echo "Applying secret $(CREDS_SECRET_NAME) in namespace $(NAMESPACE)"
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
	@echo "Ensuring kind cluster 'maintainerd' exists"
	@kind get clusters | grep -qx maintainerd || kind create cluster --name maintainerd --config hack/kind-config.yaml
	@echo "Kube context set to $(KUBECONTEXT)"
	@$(MAKE) argocd-install

.PHONY: kind-down
kind-down:
	@echo "Deleting kind cluster 'maintainerd'"
	@kind delete cluster --name maintainerd || true

# ---- Kubernetes resources for bootstrap ----
.PHONY: ensure-ns
ensure-ns:
	@kubectl get ns $(NAMESPACE) >/dev/null 2>&1 \
		|| kubectl create ns $(NAMESPACE)

.PHONY: apply-ghcr-secret
apply-ghcr-secret:
	@:${DOCKER_REGISTRY_USERNAME:?Set DOCKER_REGISTRY_USERNAME (e.g. your GitHub username)}
	@:${DOCKER_REGISTRY_PASSWORD:?Set DOCKER_REGISTRY_PASSWORD (a PAT with package:read)}
	@echo "Applying docker-registry secret 'ghcr-secret' in namespace $(NAMESPACE)"
	@kubectl -n $(NAMESPACE) create secret docker-registry ghcr-secret \
		--docker-server=$(DOCKER_REGISTRY_SERVER) \
		--docker-username=$(DOCKER_REGISTRY_USERNAME) \
		--docker-password=$(DOCKER_REGISTRY_PASSWORD) \
		$(if $(KUBECONTEXT),--context $(KUBECONTEXT)) \
		--dry-run=client -o yaml | kubectl -n $(NAMESPACE) apply -f -

.PHONY: apply-pvc
apply-pvc:
	@echo "Applying pvc.yaml to namespace $(NAMESPACE)"
	@kubectl -n $(NAMESPACE) $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) apply -f pvc.yaml

.PHONY: bootstrap-apply
bootstrap-apply:
	@echo "Applying bootstrap.yaml Job to namespace $(NAMESPACE)"
	@kubectl -n $(NAMESPACE) $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) apply -f bootstrap.yaml

.PHONY: bootstrap-wait
bootstrap-wait:
	@echo "Waiting for Job/maintainerd-bootstrap to complete in $(NAMESPACE)"
	@kubectl -n $(NAMESPACE) $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) wait --for=condition=complete --timeout=5m job/maintainerd-bootstrap

.PHONY: bootstrap-restart
bootstrap-restart:
	@echo "Deleting and re-applying bootstrap Job"
	@kubectl -n $(NAMESPACE) $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) delete job/maintainerd-bootstrap --ignore-not-found=true
	@kubectl -n $(NAMESPACE) $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) apply -f bootstrap.yaml
	@$(MAKE) bootstrap-wait

# ---- Argo CD install (cluster add-on) ----
.PHONY: argocd-install
argocd-install:
	@echo "Installing Argo CD into namespace argocd (idempotent)"
	@kubectl $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) get ns argocd >/dev/null 2>&1 || kubectl $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) create ns argocd
	@kubectl $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml
	@echo "Waiting for Argo CD core deployments to be Ready"
	@kubectl $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) -n argocd rollout status deploy/argocd-repo-server --timeout=180s || true
	@kubectl $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) -n argocd rollout status deploy/argocd-server --timeout=180s || true
	@kubectl $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) -n argocd rollout status deploy/argocd-application-controller --timeout=180s || true
	@echo "Argo CD installed. To open the UI, run: make argocd-ui"
	@echo "Initial admin password (run this):"
	@echo "  kubectl $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d; echo"

.PHONY: argocd-ui
argocd-ui:
	@echo "Port-forwarding Argo CD UI to https://localhost:8088 (Ctrl-C to stop)"
	@echo "Initial admin password (run this):"
	@echo "  kubectl $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d; echo"
	@kubectl $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) -n argocd port-forward svc/argocd-server 8088:443

.PHONY: argo-bootstrap
argo-bootstrap:
	@echo "Applying Argo CD project and applications from deploy/argocd"
	@kubectl $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) apply -f deploy/argocd/project.yaml
	@kubectl $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) apply -f deploy/argocd/app-metallb.yaml
	@kubectl $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) apply -f deploy/argocd/app-metallb-config.yaml
	@kubectl $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) apply -f deploy/argocd/app-ingress-nginx.yaml
	@kubectl $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) apply -f deploy/argocd/app-maintainerd-dev.yaml
	@kubectl $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) apply -f deploy/argocd/app-maintainerd-prod.yaml
	@echo "Argo apps applied. Open the Argo CD UI to monitor syncs."

# ---- ingress controller (nginx) for kind ----
.PHONY: ingress-nginx-install
ingress-nginx-install:
	@echo "Installing ingress-nginx controller (kind provider)"
	@kubectl $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml
	@echo "Waiting for ingress controller to be Ready"
	@kubectl $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) -n ingress-nginx wait --for=condition=Ready pod -l app.kubernetes.io/component=controller --timeout=180s

# High-level: bring up full local stack for bootstrap
.PHONY: cluster-up
cluster-up: kind-up ensure-ns apply-ghcr-secret secrets apply-pvc bootstrap-apply
	@echo "Cluster ready. Use 'make bootstrap-wait' to wait for job."

.PHONY: cluster-down
cluster-down: kind-down
	@echo "Cluster 'maintainerd' deleted"

# ---- maintainerd server deployment ----
.PHONY: maintainerd-apply
maintainerd-apply: bootstrap-wait ingress-nginx-install
	@echo "Applying maintainerd.yaml to namespace $(NAMESPACE)"
	@kubectl -n $(NAMESPACE) $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) apply -f maintainerd.yaml

.PHONY: maintainerd-delete
maintainerd-delete:
	@echo "Deleting Deployment/Service 'maintainerd' from $(NAMESPACE)"
	@kubectl -n $(NAMESPACE) $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) delete deploy/maintainerd svc/maintainerd --ignore-not-found

.PHONY: maintainerd-restart
maintainerd-restart:
	@echo "Rolling out restart for Deployment/maintainerd"
	@kubectl -n $(NAMESPACE) $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) rollout restart deploy/maintainerd

.PHONY: maintainerd-port-forward
maintainerd-port-forward:
	@echo "Port-forwarding localhost:2525 -> service/maintainerd:2525"
	@kubectl -n $(NAMESPACE) $(if $(KUBECONTEXT),--context $(KUBECONTEXT)) port-forward svc/maintainerd 2525:2525
