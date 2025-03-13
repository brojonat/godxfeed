define setup_env
	$(eval ENV_FILE := $(1))
	$(eval include $(1))
	$(eval export)
endef

build-cli:
	$(call setup_env, service/.env)
	go build -o cli cmd/godxfeed/*.go

refresh-env:
	$(call setup_env, service/.env)
	./cli admin get-session-token -u "$$TW_USERNAME" -p "$$TW_PASSWORD" --env-path service/.env
	./cli admin get-streamer-token --env-path service/.env

run-http-server:
	$(call setup_env, service/.env)
	./cli run http-server \
		--symbol SPY \
		--symbol-method n-related \
		--handler-persist
