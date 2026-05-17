BIN     := tuai
PKG     := .
GOFLAGS := -trimpath
LDFLAGS := -s -w

# Optional model override (e.g. make run MODEL=opus, MODEL=claude-sonnet-4-6).
# Empty = let Claude Code pick.
MODEL ?=

.PHONY: help build run dev install fmt vet tidy test clean reset deps version

help:
	@echo "tuai — TUI wrapper for Claude Code"
	@echo ""
	@echo "Targets:"
	@echo "  make run         build and run"
	@echo "  make dev         go run (no binary)"
	@echo "  make build       compile binary to ./$(BIN)"
	@echo "  make install     go install to \$$GOBIN"
	@echo "  make fmt         gofmt all sources"
	@echo "  make vet         go vet"
	@echo "  make tidy        go mod tidy"
	@echo "  make deps        upgrade all dependencies to latest"
	@echo "  make test        run tests"
	@echo "  make clean       remove built binary"
	@echo "  make reset       delete saved sessions + config (~/.config/tuai)"
	@echo "  make version     show toolchain + dep versions"
	@echo ""
	@echo "Requires the 'claude' CLI on PATH (Claude Code) — auth & sessions handled by it."
	@echo "Optional env: CLAUDE_BIN (path), CLAUDE_MODEL (model alias/name)."
	@echo "Optional make var: MODEL=opus    e.g. make run MODEL=opus"

build:
	go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BIN) $(PKG)

run: build _checkclaude
	CLAUDE_MODEL=$(MODEL) ./$(BIN)

dev: _checkclaude
	CLAUDE_MODEL=$(MODEL) go run $(PKG)

install:
	go install $(GOFLAGS) -ldflags="$(LDFLAGS)" $(PKG)

fmt:
	gofmt -s -w .

vet:
	go vet ./...

tidy:
	go mod tidy

deps:
	go get -u ./... && go mod tidy

test:
	go test ./...

clean:
	rm -f $(BIN)

reset:
	@read -p "Delete all sessions and config in ~/.config/tuai? [y/N] " ans && \
		{ [ "$$ans" = "y" ] || [ "$$ans" = "Y" ]; } && rm -rf ~/.config/tuai || echo "cancelled"

version:
	@go version
	@go list -m charm.land/bubbletea/v2 charm.land/bubbles/v2 charm.land/lipgloss/v2 \
		github.com/alecthomas/chroma/v2
	@printf "claude:    "; (command -v $${CLAUDE_BIN:-claude} >/dev/null && $${CLAUDE_BIN:-claude} --version) || echo "not found"

_checkclaude:
	@bin=$${CLAUDE_BIN:-claude}; \
	if ! command -v $$bin >/dev/null 2>&1; then \
		echo "error: '$$bin' not found on PATH"; \
		echo "  install Claude Code: https://docs.claude.com/en/docs/claude-code"; \
		echo "  or set CLAUDE_BIN to the path of your claude binary"; \
		exit 1; \
	fi
