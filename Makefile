# Quality checks for the nosey CLI.
GO ?= go
BIN := dist/nosey
LINTER_BIN := ./custom-gcl
COVERAGE_MIN ?= 80
# Name of the generated agent skill for this CLI.
SKILL_NAME ?= $(notdir $(BIN))
# Core services have a shared coverage gate; CLI/TUI tests exercise the same services.
CORE_PACKAGES := ./internal/app ./internal/catalog ./internal/registrygen ./internal/contracts ./internal/detection ./internal/installation ./internal/process ./internal/registry ./internal/settings ./internal/storage
empty :=
space := $(empty) $(empty)
CORE_COVERPKG = $(subst $(space),$(comma),$(strip $(CORE_PACKAGES)))
comma := ,

.PHONY: registry-check check fix format format-check tidy-check lint test test-coverage build smoke repro-check cross-build run skills audit hooks-install clean

# Release platforms; keep in sync with the release.yml matrix.
CROSS_TARGETS := linux/amd64 linux/arm64

check: format-check tidy-check lint registry-check test-coverage build smoke

registry-check:
	$(GO) run ./cmd/nosey registry generate registries/default --check
	$(GO) run ./cmd/nosey registry validate registries/default --standalone

tidy-check:
	$(GO) mod tidy -diff

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
	$(GO) test -race -covermode=atomic -coverpkg=$(CORE_COVERPKG) -coverprofile=core-coverage.out ./cmd/nosey ./internal/ui $(CORE_PACKAGES)
	@$(GO) tool cover -func=core-coverage.out | awk -v min=$(COVERAGE_MIN) \
	  '/^total:/ { gsub(/%/, "", $$3); printf "Core coverage: %.1f%% (minimum: %s%%)\n", $$3, min; found=1; if ($$3 + 0 < min) exit 1 } END { if (!found) exit 1 }'

build:
	@mkdir -p dist
	$(GO) build -trimpath -o $(BIN) ./cmd/nosey

# The built binary must start and report the version declared in package.toml.
smoke: build
	@want="$$(sed -n 's/^version *= *"\(.*\)"/\1/p' internal/package/package.toml)"; \
	got="$$($(BIN) --version 2>&1)"; \
	if [ "$$got" != "$$want" ]; then echo "version mismatch: binary $$got, package.toml $$want" >&2; exit 1; fi; \
	$(BIN) --help > /dev/null

# Two clean builds of the same sources must be byte-identical.
repro-check:
	@mkdir -p dist
	CGO_ENABLED=0 $(GO) build -trimpath -o dist/repro-a ./cmd/nosey
	CGO_ENABLED=0 $(GO) build -trimpath -o dist/repro-b ./cmd/nosey
	cmp dist/repro-a dist/repro-b
	@echo 'Reproducible build: OK'

# Every release platform (Linux only) must at least compile before a tag is pushed.
cross-build:
	@set -e; for target in $(CROSS_TARGETS); do \
	  os="$${target%/*}"; arch="$${target#*/}"; \
	  echo "building $$os/$$arch"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -o /dev/null ./cmd/nosey; \
	done

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
	rm -rf dist core-coverage.out $(LINTER_BIN)
