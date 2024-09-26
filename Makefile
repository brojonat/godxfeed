define setup_env
	$(eval ENV_FILE := $(1))
	$(eval include $(1))
	$(eval export)
endef

build-cli:
	go build -o cli cmd/godxfeed/*.go

run-http-server:
	$(call setup_env, service/.env)
	./cli run http-server
