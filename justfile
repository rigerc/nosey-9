# Justfile setup
# ================================================================================
set shell := ["zsh", "-cu"]

PACKAGE := "go-cli-template"
PACKAGE_BIN := "./bin/" + PACKAGE
PACKAGE_CMD := "./cmd/" + PACKAGE

cli_pack := "bin/go-cli-package"
pack_version := "v0.3.7"

_install-pack:
	@if [ ! -f ./bin/go-cli-package ]; then \
		echo "📥 Installing go-cli-package {{pack_version}}..."; \
		GOBIN="{{justfile_directory()}}/bin" "$(go env GOROOT)/bin/go" install github.com/imdevan/go-cli-package/cmd/go-cli-package@{{pack_version}}; \
	fi



# Build
# ================================================================================

build:
	go build -o {{PACKAGE_BIN}} {{PACKAGE_CMD}}
	@size=$(stat -c %s {{PACKAGE_BIN}} 2>/dev/null || stat -f %z {{PACKAGE_BIN}} 2>/dev/null); \
	if [ -n "$size" ]; then \
		echo "Build size: $(awk "BEGIN {printf \"%.2f MB\", $size/1048576}")"; \
	else \
		echo "Build size: unavailable"; \
	fi

build-run:
	go build -o {{PACKAGE_BIN}} {{PACKAGE_CMD}} && {{PACKAGE_BIN}}

watch:
	@rg --files | entr -r sh -c 'sleep 0.5; go build -o {{PACKAGE_BIN}} {{PACKAGE_CMD}}'

dev-build:
	go build -gcflags "all=-N -l" -o {{PACKAGE_BIN}} {{PACKAGE_CMD}}

# Install local build globally
install:
	install -m 0755 {{PACKAGE_BIN}} /usr/local/bin/{{PACKAGE}}

# Uninstall local build globally
uninstall:
	rm -f /usr/local/bin/{{PACKAGE}}

test:
	go test ./...

test-verbose:
	go test -v ./...

clean:
	rm -rf bin

# Examples
# ================================================================================

# Run the inline go-clack form gallery (all forms, or one demo name)
forms demo="all":
	go run ./examples/forms {{demo}}

# Agent skills (generated from the compiled Cobra CLI)
# ================================================================================

skills:
	make skills

# Package management
# ================================================================================

# Rename template based on internal/package/package.toml or pass --package 
sync args="": _install-pack
  {{cli_pack}} sync {{args}}

# Github tag management

tag-list: build _install-pack
	{{cli_pack}} tag list

tag version="": build _install-pack
	{{cli_pack}} tag create {{version}}

tag-delete version="": build _install-pack
	{{cli_pack}} tag delete {{version}}

# Github release (calls tag)

release version="": build _install-pack
	{{cli_pack}} release {{version}}

# Pipeline init

init-homebrew: build _install-pack
	{{cli_pack}} init homebrew

init-aur: build _install-pack
	{{cli_pack}} init aur

init: build _install-pack
	{{cli_pack}} init all

# Package updates

update-homebrew version="": build _install-pack
	{{cli_pack}} update homebrew {{version}}

update-aur version="": build _install-pack
	{{cli_pack}} update aur {{version}}

update version="": build _install-pack
	{{cli_pack}} update all {{version}}


# Publish

publish-homebrew version="": build _install-pack
	{{cli_pack}} publish homebrew {{version}}

publish-aur version="": build _install-pack
	{{cli_pack}} publish aur {{version}}

publish version="": build _install-pack
	{{cli_pack}} publish all {{version}}



# Agent-friendly verification (see Makefile for pinned linter/coverage tooling)
# ================================================================================

check:
	make check

format:
	make format

format-check:
	make format-check

lint:
	make lint

test-coverage:
	make test-coverage

audit:
	make audit

hooks-install:
	make hooks-install
