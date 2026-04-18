define setup_env
	$(eval ENV_FILE := $(1))
	$(eval include $(1))
	$(eval export)
endef

# Dev targets source service/.env.dev (sandbox); k8s targets source
# service/.env.prod (production). Keeps sandbox and prod creds separate.

build-cli:
	go build -o cli cmd/godxfeed/*.go

# Fetch a new godxfeed bearer JWT (used by the web UI). tastytrade access
# tokens are refreshed automatically by the running server using the OAuth
# Personal Grant credentials in service/.env.dev — no daily ritual required.
refresh-auth-token: build-cli
	$(call setup_env, service/.env.dev)
	./cli admin get-bearer-token --env-file service/.env.dev

# Sanity-check the OAuth wiring by requesting a dxFeed streamer token against
# the sandbox.
check-oauth: build-cli
	$(call setup_env, service/.env.dev)
	./cli admin get-streamer-token

# Run the HTTP server in full (non-minimal) mode with NATS auth callout active,
# but without subscribing to any dxfeed symbols. This is the workhorse dev
# target — use it when market is closed or when you want to drive data via the
# dummy publisher. Pair with `run-http-debug-server` when you actually want to
# subscribe to live quotes during market hours.
#
# ANALYTIC_SINKS (e.g. postgres://...) defined in service/.env.dev is picked up
# automatically via the --analytic-sink flag.
run-http-dev: build-cli
	@mkdir -p logs
	$(call setup_env, service/.env.dev)
	./cli run http-server \
		--dev-mode \
		--log-level -4 \
		--analytic-sink "$$DATABASE_URL" \
		2>&1 | tee logs/http.log

run-http-debug-server: build-cli
	@mkdir -p logs
	$(call setup_env, service/.env.dev)
	./cli run http-server \
		--symbol SPY \
		--symbol-method n-related \
		--handler-persist \
		--dev-mode \
		--log-level -4 2>&1 | tee logs/http.log

run-http-server-minimal: build-cli
	@mkdir -p logs
	$(call setup_env, service/.env.dev)
	./cli run http-server --minimal-setup 2>&1 | tee logs/http.log

run-nats-server:
	@mkdir -p logs
	nats-server -c service/nats/nats.conf 2>&1 | tee logs/nats.log

run-nats-dummy-publisher: build-cli
	@mkdir -p logs
	$(call setup_env, service/.env.dev)
	./cli debug publish-nats --nats-topic godxfeed.SPY --interval 150ms 2>&1 | tee logs/publisher.log

tail-http-log:
	tail -f logs/http.log

tail-nats-log:
	tail -f logs/nats.log

# -----------------------------------------------------------------------------
# tmux dev stack
#
# Session name is intentionally `godxfeed-dev` (not `godxfeed`) so that the
# dev services never collide with a personal tmux session you might have
# named after the project — `make dev-down` nuking your editor is a real
# hazard otherwise. If you want to change this (multiple devs on one box,
# etc.), override TMUX_SESSION on the command line: `TMUX_SESSION=foo make dev-up`.
#
# `make dev-up`      — start the session with nats, http, publisher windows
# `make dev-attach`  — attach to the session
# `make dev-down`    — kill the session (and every process in it)
# `make dev-status`  — show session + window state
# -----------------------------------------------------------------------------
TMUX_SESSION ?= godxfeed-dev

.PHONY: dev-up dev-down dev-attach dev-status

dev-up:
	@if tmux has-session -t $(TMUX_SESSION) 2>/dev/null; then \
		echo "tmux session '$(TMUX_SESSION)' already running — attach with 'make dev-attach' or tear down with 'make dev-down'"; \
		exit 1; \
	fi
	@mkdir -p logs
	tmux new-session -d -s $(TMUX_SESSION) -n nats -c $(CURDIR)
	tmux send-keys -t $(TMUX_SESSION):nats 'make run-nats-server' C-m
	@sleep 1
	tmux new-window -t $(TMUX_SESSION) -n http -c $(CURDIR)
	tmux send-keys -t $(TMUX_SESSION):http 'make run-http-dev' C-m
	@sleep 2
	tmux new-window -t $(TMUX_SESSION) -n publisher -c $(CURDIR)
	tmux send-keys -t $(TMUX_SESSION):publisher 'make run-nats-dummy-publisher' C-m
	tmux select-window -t $(TMUX_SESSION):http
	@echo "Started tmux session '$(TMUX_SESSION)'. Attach with: make dev-attach"

dev-attach:
	tmux attach-session -t $(TMUX_SESSION)

dev-down:
	@current=$$(tmux display-message -p '#{session_name}' 2>/dev/null); \
	if [ "$$current" = "$(TMUX_SESSION)" ]; then \
		echo "refusing to kill '$(TMUX_SESSION)' — you're currently attached to it. Detach first (Ctrl-b d), then re-run."; \
		exit 1; \
	fi; \
	tmux kill-session -t $(TMUX_SESSION) 2>/dev/null && echo "Stopped tmux session '$(TMUX_SESSION)'" || echo "No session '$(TMUX_SESSION)' running"

dev-status:
	@tmux has-session -t $(TMUX_SESSION) 2>/dev/null && tmux list-windows -t $(TMUX_SESSION) || echo "session '$(TMUX_SESSION)' not running"

# Docker build targets
docker-build:
	docker build -t godxfeed-cli:latest -f service/Dockerfile .

docker-push:
	@if [ -z "$$DOCKER_REPO" ]; then \
		echo "Error: DOCKER_REPO environment variable must be set"; \
		exit 1; \
	fi
	docker tag godxfeed-cli:latest $$DOCKER_REPO/godxfeed-cli:$${CLI_IMG_TAG:-latest}
	docker push $$DOCKER_REPO/godxfeed-cli:$${CLI_IMG_TAG:-latest}

# Kubernetes deployment targets
.PHONY: k8s-deploy-prod k8s-delete-prod k8s-status k8s-preview-prod k8s-update-secrets k8s-logs k8s-describe k8s-restart k8s-deploy-nats k8s-delete-nats k8s-nats-status k8s-nats-logs k8s-nats-describe k8s-nats-restart

k8s-deploy-prod:
	@if [ -z "$$DOCKER_REPO" ] || [ -z "$$CLI_IMG_TAG" ]; then \
		echo "Error: DOCKER_REPO and CLI_IMG_TAG environment variables must be set"; \
		exit 1; \
	fi
	@if [ ! -f service/.env.prod ]; then \
		echo "Error: service/.env.prod not found. Run 'make env-prod' first"; \
		exit 1; \
	fi
	$(call setup_env, service/.env.prod)
	@$(MAKE) docker-push
	kustomize build --load-restrictor=LoadRestrictionsNone service/k8s/prod | \
	sed -e "s;{{DOCKER_REPO}};$(DOCKER_REPO);g" \
		-e "s;{{CLI_IMG_TAG}};$(CLI_IMG_TAG);g" | \
		kubectl apply -f -
	kubectl rollout restart deployment godxfeed-backend

k8s-delete-prod:
	kubectl delete -f service/k8s/prod/ingress.yaml || true
	kubectl delete -f service/k8s/prod/server.yaml || true
	kubectl delete secret godxfeed-secret-server-envs || true

# Environment file management. Seed service/.env.prod from .env.dev so the
# operator only has to edit the handful of values that differ between sandbox
# and prod (OAuth URL/host, OAuth creds, NATS URLs, etc.).
env-prod:
	@if [ ! -f service/.env.prod ]; then \
		cp service/.env.dev service/.env.prod; \
		echo "Created service/.env.prod from service/.env.dev — edit prod-specific values before deploying."; \
	else \
		echo "service/.env.prod already exists"; \
	fi

# Helper target to check deployment status
k8s-status:
	@echo "=== Deployment Status ==="
	kubectl get deployment godxfeed-backend -o wide
	@echo "\n=== Pods Status ==="
	kubectl get pods -l app=godxfeed-backend
	@echo "\n=== Ingress Status ==="
	kubectl get ingress godxfeed-backend-ingress

# Preview kustomize output
k8s-preview-prod:
	@if [ -z "$$DOCKER_REPO" ] || [ -z "$$CLI_IMG_TAG" ]; then \
		echo "Error: DOCKER_REPO and CLI_IMG_TAG environment variables must be set"; \
		exit 1; \
	fi
	kustomize build --load-restrictor=LoadRestrictionsNone service/k8s/prod | \
	sed -e "s;{{DOCKER_REPO}};$(DOCKER_REPO);g" \
		-e "s;{{CLI_IMG_TAG}};$(CLI_IMG_TAG);g"

# Update secrets
k8s-update-secrets:
	@if [ ! -f service/.env.prod ]; then \
		echo "Error: service/.env.prod not found. Run 'make env-prod' first"; \
		exit 1; \
	fi
	kubectl create secret generic godxfeed-secret-server-envs \
		--from-env-file=service/.env.prod \
		--dry-run=client -o yaml | kubectl apply -f -

# View logs
k8s-logs:
	kubectl logs -f deployment/godxfeed-backend

# Describe resources
k8s-describe:
	kubectl describe deployment godxfeed-backend
	kubectl describe service godxfeed-backend
	kubectl describe ingress godxfeed-backend-ingress

# Restart deployment
k8s-restart:
	kubectl rollout restart deployment godxfeed-backend

# Port forwarding for local development
k8s-port-forward:
	kubectl port-forward svc/godxfeed-backend 8080:80

# NATS Deployment targets
k8s-deploy-nats:
	@if [ ! -f service/.env.prod ]; then \
		echo "Error: service/.env.prod not found. Run 'make env-prod' first"; \
		exit 1; \
	fi
	$(call setup_env, service/.env.prod)
	kustomize build --load-restrictor=LoadRestrictionsNone service/nats/k8s/prod | kubectl apply -f -
	kubectl rollout restart deployment nats-server

k8s-delete-nats:
	kubectl delete -f service/nats/k8s/prod/ingress.yaml || true
	kubectl delete -f service/nats/k8s/prod/server.yaml || true
	kubectl delete -f service/nats/k8s/prod/storage.yaml || true
	kubectl delete configmap nats-config || true
	kubectl delete secret nats-secrets || true

k8s-nats-status:
	@echo "=== NATS Deployment Status ==="
	kubectl get deployment nats-server -o wide
	@echo "\n=== NATS Pods Status ==="
	kubectl get pods -l app=nats-server
	@echo "\n=== NATS Ingress Status ==="
	kubectl get ingress nats-server-ingress

k8s-nats-logs:
	kubectl logs -f deployment/nats-server

k8s-nats-describe:
	kubectl describe deployment nats-server
	kubectl describe service nats-server
	kubectl describe ingress nats-server-ingress
	kubectl describe configmap nats-config

k8s-nats-restart:
	kubectl rollout restart deployment nats-server

# Port forwarding for NATS
k8s-nats-port-forward:
	kubectl port-forward svc/nats-server 4222:4222
