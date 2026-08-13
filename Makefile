.DEFAULT_GOAL := help
BIN := shellbeam
PKG := ./cmd/shellbeam
PIDFILE := .build/run/daemon.pid
LOG := /tmp/shellbeam-daemon.log

.PHONY: help build test vet fmt fmt-check tidy modverify doctor daemon run-mcp \
	daemon-start daemon-stop daemon-restart daemon-status \
	hardening security mcp-local vulncheck devctl-check devctl-verify \
	test-dirty build-dirty release-evidence package verify-package clean ci

help: ## List available targets
	@grep -E '^[a-zA-Z0-9_-]+:.*## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN{FS=":.*## "}{printf "  %-16s %s\n", $$1, $$2}'

build: ## Build the shellbeam binary at repo root
	go build -trimpath -buildvcs=false -o $(BIN) $(PKG)

test: ## Run the full test suite
	go test -count=1 ./...

vet: ## Run go vet
	go vet ./...

fmt: ## Reformat all Go files with gofmt
	gofmt -w .

fmt-check: ## Fail if any Go file is not gofmt-formatted
	@files="$$(gofmt -l .)"; \
	if [ -n "$$files" ]; then echo "not gofmt-formatted:"; echo "$$files"; exit 1; fi

tidy: ## Run go mod tidy
	go mod tidy

modverify: ## Verify module checksums against go.sum
	go mod verify

doctor: build ## Build then run `shellbeam doctor --json`
	./$(BIN) doctor --json

daemon: build ## Build then run the daemon in the foreground
	./$(BIN) daemon

daemon-start: build ## Start the daemon in the background (pidfile: .build/run/daemon.pid, log: /tmp/shellbeam-daemon.log)
	@mkdir -p $(dir $(PIDFILE))
	@if [ -f $(PIDFILE) ] && kill -0 "$$(cat $(PIDFILE))" 2>/dev/null; then \
		echo "daemon already running (pid $$(cat $(PIDFILE)))"; \
	else \
		nohup ./$(BIN) daemon >$(LOG) 2>&1 & echo $$! > $(PIDFILE); \
		sleep 1; \
		echo "daemon started (pid $$(cat $(PIDFILE))), log: $(LOG)"; \
	fi

daemon-stop: ## Stop the background daemon started by `make daemon-start`
	@if [ -f $(PIDFILE) ] && kill -0 "$$(cat $(PIDFILE))" 2>/dev/null; then \
		kill "$$(cat $(PIDFILE))" && echo "daemon stopped (pid $$(cat $(PIDFILE)))"; \
	else \
		echo "daemon not running (or pidfile stale)"; \
	fi; \
	rm -f $(PIDFILE)

daemon-restart: ## Stop then start the background daemon
	@$(MAKE) --no-print-directory daemon-stop
	@$(MAKE) --no-print-directory daemon-start

daemon-status: build ## Show whether the background daemon is running, plus `doctor --json`
	@if [ -f $(PIDFILE) ] && kill -0 "$$(cat $(PIDFILE))" 2>/dev/null; then \
		echo "daemon running (pid $$(cat $(PIDFILE)))"; \
	else \
		echo "daemon not running"; \
	fi
	@./$(BIN) doctor --json

run-mcp: build ## Build then run the MCP stdio server (Ctrl-C to stop)
	./$(BIN) mcp

hardening: build ## Race suite + bounded fuzz campaigns (scripts/test-hardening.sh)
	GOCACHE="$$(go env GOCACHE)" scripts/test-hardening.sh

security: ## Module verify, forbidden-pattern scan, log-redaction checks, govulncheck if present
	scripts/test-security.sh

mcp-local: ## MCP SDK in-memory conformance test
	scripts/test-mcp-local.sh

vulncheck: ## Run govulncheck (installs it first if missing)
	@command -v govulncheck >/dev/null 2>&1 || go install golang.org/x/vuln/cmd/govulncheck@latest
	"$$(go env GOPATH)/bin/govulncheck" ./...

devctl-check: ## devctl repository check (writes a receipt under .build/receipts/)
	go run ./tools/devctl check

devctl-verify: ## devctl check + test (writes a receipt under .build/receipts/)
	go run ./tools/devctl verify

test-dirty: ## Run only tests affected by the current source delta
	go run ./tools/devctl test --dirty --base $${SHELLBEAM_BASE_REF:-origin/main}

build-dirty: ## Incrementally build the current source through devctl
	go run ./tools/devctl build --dirty --base $${SHELLBEAM_BASE_REF:-origin/main}

release-evidence: ## Regenerate .build/release/release-evidence.json via devctl
	go run ./tools/devctl release-evidence

package: release-evidence ## Build the clean-room source ZIP into dist/ (requires a clean working tree)
	scripts/package-source.sh

verify-package: ## Clean-room verify a packaged ZIP: make verify-package ARCHIVE=dist/shellbeam-v1-source-<sha>.zip
	@test -n "$(ARCHIVE)" || { echo "usage: make verify-package ARCHIVE=dist/shellbeam-v1-source-<sha>.zip"; exit 2; }
	scripts/verify-source-package.sh "$(ARCHIVE)"

clean: ## Remove local build output
	rm -f $(BIN)
	rm -rf .build dist

ci: fmt-check vet test security ## Everything CI-equivalent expects on a normal PR
