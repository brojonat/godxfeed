define setup_env
	$(eval ENV_FILE := $(1))
	$(eval include $(1))
	$(eval export)
endef

build-cli:
	$(call setup_env, service/.env)
	go build -o cli cmd/godxfeed/*.go

refresh-env:
	./cli admin get-bearer-token --env-file service/.env
	$(call setup_env, service/.env)
	@if [ -z "$$TW_USERNAME" ] || [ -z "$$TW_PASSWORD" ]; then \
		echo "Error: TW_USERNAME and TW_PASSWORD environment variables must be set"; \
		exit 1; \
	fi
	./cli admin get-session-token -u "$$TW_USERNAME" -p "$$TW_PASSWORD" --env-file service/.env
	$(call setup_env, service/.env)
	./cli admin get-streamer-token --env-file service/.env

run-http-debug-server:
	$(call setup_env, service/.env)
	./cli run http-server \
		--symbol SPY \
		--symbol-method n-related \
		--handler-persist \
		--dev-mode \
		--log-level -4

run-http-server-minimal:
	$(call setup_env, service/.env)
	./cli run http-server --minimal-setup

run-nats-server:
	nats-server -c service/nats.conf

make-nats-dummy-publisher:
	./cli debug publish-nats --nats-topic godxfeed.SPY --interval 150ms
