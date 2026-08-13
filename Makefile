.DEFAULT_GOAL := help
BIN := shellbeam
PKG := ./cmd/shellbeam

.PHONY: help build test vet fmt fmt-check tidy modverify doctor daemon run-mcp \
	hardening security mcp-local vulncheck devctl-check devctl-verify \
	release-evidence package verify-package clean ci

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
