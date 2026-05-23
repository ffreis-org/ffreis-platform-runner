BINARY           := platform-runner
MODULE           := github.com/ffreis/platform-runner
BUILD_DIR        := bin
CMD_PKG          := ./cmd/$(BINARY)
GOFLAGS          := -trimpath
LDFLAGS          := -w -s

# Container engine: prefers podman, falls back to docker.
CONTAINER_ENGINE := $(shell ./scripts/run which 2>/dev/null || command -v podman 2>/dev/null || command -v docker 2>/dev/null)
IMAGE_NAME       := ghcr.io/ffreis/platform-runner
IMAGE_TAG        ?= dev

GITLEAKS         ?= gitleaks
LEFTHOOK_VERSION ?= 1.7.10

MUTATION_PACKAGES ?= ./internal/runner/... ./internal/executor/...
MUTATION_THRESHOLD ?= 60
LEFTHOOK_DIR     ?= $(CURDIR)/.bin
LEFTHOOK_BIN     ?= $(LEFTHOOK_DIR)/lefthook

.PHONY: all build install test test-short vet lint tidy clean check fmt fmt-check sec ci \
        validate plan mutation-test help \
        container-build container-test container-run container-push \
        secrets-scan-staged lefthook-bootstrap lefthook-install lefthook-run lefthook

all: build

## ── Local Go targets ────────────────────────────────────────────────────────

## build: compile the binary into bin/
build:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) $(CMD_PKG)

## install: install the binary to GOPATH/bin
install:
	go install $(GOFLAGS) -ldflags "$(LDFLAGS)" ./cmd/$(BINARY)

## test: run all tests with race detector
test:
	# NOTE: -shuffle=on is intentionally omitted here. Enabling it surfaces a
	# real pre-existing test-isolation bug in cmd/: captureStdout() in
	# commands_test.go swaps os.Stdout globally, and t.Parallel() tests in
	# root_test.go can interleave with captured regions, corrupting the
	# captured output. Re-enable -shuffle=on once captureStdout is made
	# concurrency-safe (or the t.Parallel calls are removed).
	go test ./... -v -race -count=1

## test-short: run unit tests (no live AWS)
test-short:
	go test ./... -short -v -count=1

## vet: run go vet
vet:
	go vet ./...

## lint: run golangci-lint
lint:
	golangci-lint run ./...

## tidy: tidy and verify go modules
tidy:
	go mod tidy
	go mod verify

## clean: remove build artefacts
clean:
	rm -rf $(BUILD_DIR)

## check: tidy + vet + test-short (fast pre-commit gate)
check: tidy vet test-short

## fmt-check: fail if any files need gofmt (mirrors CI)
fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
	  printf "The following files need gofmt:\n%s\n\nFix with: gofmt -w .\n" "$$unformatted"; \
	  exit 1; \
	fi

## sec: run govulncheck for known CVEs in dependencies
sec:
	govulncheck ./...

## ci: local equivalent of CI gate (fmt-check + vet + lint + nakedgo + test + sec)
ci: fmt-check vet lint nakedgo test sec

## nakedgo: flag goroutines that don't begin with defer recover()
##   Pulls the analyzer fresh on each run; no permanent dep added to go.mod.
nakedgo:
	go run github.com/FelipeFuhr/ffreis-platform-go-analyzers/cmd/nakedgo@latest ./...

## fmt: format all Go files in place
fmt:
	gofmt -w .

## validate: static analysis and compilation check
validate:
	go vet ./...
	go build $(CMD_PKG)

## plan: not applicable — use 'make validate' or 'make ci' for Go repos
plan:
	@echo "INFO: 'plan' is Terraform-specific and does not apply to Go repos."
	@echo "      To verify compilation: make validate"
	@echo "      For a full CI-equivalent gate: make ci"

## secrets-scan-staged: scan staged diff for secrets
secrets-scan-staged:
	@command -v $(GITLEAKS) >/dev/null 2>&1 || (echo "Missing tool: $(GITLEAKS). Install: https://github.com/gitleaks/gitleaks#installing" && exit 1)
	$(GITLEAKS) protect --staged --redact

## 
PLATFORM_STANDARDS_SHA := 3f7326412e455e6ec3b1ab6f5b721ff071c6254c
PLATFORM_STANDARDS_RAW := https://raw.githubusercontent.com/FelipeFuhr/ffreis-platform-standards

HOOK_SCRIPTS := \
	check_merge_markers.sh \
	check_large_files.sh \
	check_binary_files.sh \
	check_commit_msg.sh \
	check_required_tools.sh

hook-scripts: ## Download bootstrap + hook scripts from ffreis-platform-standards
	@mkdir -p scripts/hooks
	@curl -fsSL "$(PLATFORM_STANDARDS_RAW)/$(PLATFORM_STANDARDS_SHA)/lefthook/bootstrap_lefthook.sh" \
		-o scripts/bootstrap_lefthook.sh && chmod +x scripts/bootstrap_lefthook.sh
	@for script in $(HOOK_SCRIPTS); do \
		curl -fsSL "$(PLATFORM_STANDARDS_RAW)/$(PLATFORM_STANDARDS_SHA)/lefthook/scripts/$$script" \
			-o "scripts/hooks/$$script" && chmod +x "scripts/hooks/$$script"; \
	done
	@echo "Hook scripts downloaded."

## lefthook-bootstrap: download lefthook binary into ./.bin (after fetching hook scripts)
lefthook-bootstrap: hook-scripts
	LEFTHOOK_VERSION="$(LEFTHOOK_VERSION)" BIN_DIR="$(LEFTHOOK_DIR)" bash ./scripts/bootstrap_lefthook.sh

## lefthook-install: install git hooks (runs bootstrap first)
lefthook-install: lefthook-bootstrap
	@if [ -x "$(LEFTHOOK_BIN)" ] && [ -x ".git/hooks/pre-commit" ] && [ -x ".git/hooks/pre-push" ] && [ -x ".git/hooks/commit-msg" ]; then \
		echo "lefthook hooks already installed"; \
		exit 0; \
	fi
	LEFTHOOK="$(LEFTHOOK_BIN)" "$(LEFTHOOK_BIN)" install

## lefthook-run: run all hooks locally (pre-commit + commit-msg + pre-push)
lefthook-run: lefthook-bootstrap
	LEFTHOOK="$(LEFTHOOK_BIN)" "$(LEFTHOOK_BIN)" run pre-commit
	@tmp_msg="$$(mktemp)"; \
	echo "chore(hooks): validate commit-msg hook" > "$$tmp_msg"; \
	LEFTHOOK="$(LEFTHOOK_BIN)" "$(LEFTHOOK_BIN)" run commit-msg -- "$$tmp_msg"; \
	rm -f "$$tmp_msg"
	LEFTHOOK="$(LEFTHOOK_BIN)" "$(LEFTHOOK_BIN)" run pre-push

## lefthook: install hooks and run them
lefthook: lefthook-bootstrap lefthook-install lefthook-run

## ── Container targets (podman or docker via scripts/run) ────────────────────

## container-build: build the final production image
container-build:
	./scripts/run build \
	  --target final \
	  --tag $(IMAGE_NAME):$(IMAGE_TAG) \
	  --file Containerfile \
	  .

## container-test: build the test stage and run all tests inside the container
container-test:
	./scripts/run build \
	  --target test \
	  --tag $(IMAGE_NAME):test \
	  --file Containerfile \
	  .

## container-run: run the production image (pass args via ARGS=)
container-run:
	./scripts/run run --rm $(IMAGE_NAME):$(IMAGE_TAG) $(ARGS)

## container-push: push image to registry (requires login)
container-push: container-build
	./scripts/run push $(IMAGE_NAME):$(IMAGE_TAG)

## container-shell: open a shell in the builder stage for debugging
container-shell:
	./scripts/run run --rm -it \
	  --entrypoint /bin/sh \
	  $(IMAGE_NAME):test

## mutation-test: run mutation testing with gremlins (slow — intended for CI/weekly)
mutation-test: ## Run mutation testing with gremlins (slow — CI only)
	@which gremlins >/dev/null 2>&1 || go install github.com/go-gremlins/gremlins/cmd/gremlins@latest
	gremlins unleash --threshold-efficacy $(MUTATION_THRESHOLD) $(MUTATION_PACKAGES)

## help: print available targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'
