# Quality checks from go-agentic-template, adapted to the larger CLI codebase.
GO ?= go
BIN := dist/nosey
LINTER_BIN := ./custom-gcl
COVERAGE_FILE := coverage.out
COVERAGE_MIN ?= 80
# Generated CLI agent skills belong to each downstream app, not the template.
SKILL_NAME ?= $(notdir $(BIN))
# Core services have a shared coverage gate; CLI/TUI tests exercise the same services.
RUNTIME_PACKAGES := ./internal/runtimeconfig/... ./internal/logging/... ./internal/greeting/...
CORE_PACKAGES := ./internal/app ./internal/catalog ./internal/registrygen ./internal/contracts ./internal/detection ./internal/installation ./internal/process ./internal/registry ./internal/settings ./internal/storage
empty :=
space := $(empty) $(empty)
CORE_COVERPKG = $(subst $(space),$(comma),$(strip $(CORE_PACKAGES)))
comma := ,

.PHONY: registry-check check fix format format-check lint test test-coverage build run skills audit hooks-install clean

check: format-check lint registry-check test-coverage build

registry-check:
	$(GO) run ./cmd/nosey registry generate registries/default --check

fix: format

format:
	@find cmd internal examples scripts -type f -name '*.go' -exec gofmt -w {} +

format-check:
	@unformatted="$$(find cmd internal examples scripts -type f -name '*.go' -exec gofmt -l {} +)"; \
	if [ -n "$$unformatted" ]; then \
	  printf 'Files need gofmt:\n%s\n' "$$unformatted"; exit 1; \
	fi

# Lint every package with the plugin-based anti-slop custom linter; go vet
# covers the whole project as well, and all tests run with -race.
$(LINTER_BIN): .custom-gcl.yml
	@command -v golangci-lint >/dev/null 2>&1 || { \
	  echo 'golangci-lint v2.15.0 is required for the custom linter'; exit 1; \
	}
	golangci-lint custom

lint: $(LINTER_BIN)
	$(GO) vet ./...
	$(LINTER_BIN) run ./...

test:
	$(GO) test -race ./...

test-coverage:
	$(GO) test -race ./...
	$(GO) test -race -covermode=atomic -coverprofile=$(COVERAGE_FILE) $(RUNTIME_PACKAGES)
	@$(GO) tool cover -func=$(COVERAGE_FILE) | awk -v min=$(COVERAGE_MIN) \
	  '/^total:/ { gsub(/%/, "", $$3); printf "Runtime coverage: %.1f%% (minimum: %s%%)\n", $$3, min; found = 1; if ($$3 + 0 < min) exit 1 } \
	   END { if (!found) { print "No coverage total found" > "/dev/stderr"; exit 1 } }'
	$(GO) test -race -covermode=atomic -coverpkg=$(CORE_COVERPKG) -coverprofile=core-coverage.out ./cmd/nosey ./internal/ui $(CORE_PACKAGES)
	@$(GO) tool cover -func=core-coverage.out | awk -v min=$(COVERAGE_MIN) \
	  '/^total:/ { gsub(/%/, "", $$3); printf "Core coverage: %.1f%% (minimum: %s%%)\n", $$3, min; found=1; if ($$3 + 0 < min) exit 1 } END { if (!found) exit 1 }'

build:
	@mkdir -p dist
	$(GO) build -trimpath -o $(BIN) ./cmd/nosey

run:
	$(GO) run ./cmd/nosey

# Regenerate only this CLI's skill; leave other .agents/skills entries untouched.
# Generate from the live Cobra tree through the built CLI's skillgen subcommand.
skills: build
	@set -eu; \
	name="$(SKILL_NAME)"; \
	case "$$name" in ''|*[!a-z0-9-]*|-*|*-) echo 'SKILL_NAME must be a lowercase, hyphenated CLI name' >&2; exit 1;; esac; \
	tmp_dir="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp_dir"' EXIT HUP INT TERM; \
	"$(abspath $(BIN))" skills generate --dir "$$tmp_dir" --skill-name "$$name"; \
	test -s "$$tmp_dir/$$name/SKILL.md"; \
	mkdir -p .agents/skills; \
	rm -rf ".agents/skills/$$name"; \
	mv "$$tmp_dir/$$name" ".agents/skills/$$name"

audit:
	@command -v govulncheck >/dev/null 2>&1 || { \
	  echo 'govulncheck is required: https://go.dev/doc/tutorial/govulncheck'; exit 1; \
	}
	govulncheck ./...

hooks-install:
	git config --local core.hooksPath .githooks

clean:
	rm -rf dist $(COVERAGE_FILE) core-coverage.out $(LINTER_BIN)
