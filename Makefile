.PHONY: up down clean poller poller-build poller-install poller-release logs test

# Output directory for cross-compiled release artifacts. Names match the
# assets install.sh expects under github.com/.../releases/latest/download/.
DIST_DIR := dist
POLLER_PKG := ./cmd/poller

up:
	docker compose -f infrastructure/docker-compose.yml up --build -d

down:
	docker compose -f infrastructure/docker-compose.yml down

# Stops the stack, removes the SQLite-bearing named volume, and wipes the
# host poller's offset state. Use to start from a fully empty world.
clean:
	docker compose -f infrastructure/docker-compose.yml down -v
	rm -f $(HOME)/.supervaisor/state.json
	@echo "cleaned: docker volume + $(HOME)/.supervaisor/state.json"

logs:
	docker compose -f infrastructure/docker-compose.yml logs -f

poller:
	cd poller && go run ./cmd/poller

poller-build:
	cd poller && go build -o supervaisor $(POLLER_PKG)
	@echo "Built poller/supervaisor"

poller-install:
	cd poller && go build -o $(HOME)/.local/bin/supervaisor-poller $(POLLER_PKG)
	@echo "Installed to $(HOME)/.local/bin/supervaisor-poller"
	@echo "Run: supervaisor-poller"

# Cross-compile every supported (OS,arch) into dist/. CGO is off so the
# binaries are statically linked and portable across machines of the same
# (OS,arch). Asset filenames match what install.sh fetches from the release.
poller-release:
	@mkdir -p $(DIST_DIR)
	cd poller && GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -o ../$(DIST_DIR)/supervaisor-darwin-arm64 $(POLLER_PKG)
	cd poller && GOOS=darwin  GOARCH=amd64 CGO_ENABLED=0 go build -o ../$(DIST_DIR)/supervaisor-darwin-amd64 $(POLLER_PKG)
	cd poller && GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -o ../$(DIST_DIR)/supervaisor-linux-arm64  $(POLLER_PKG)
	cd poller && GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -o ../$(DIST_DIR)/supervaisor-linux-amd64  $(POLLER_PKG)
	@echo "Release binaries in $(DIST_DIR)/:"
	@ls -lh $(DIST_DIR)/

test:
	cd backend && go test ./...
	cd poller  && go test ./...
	cd frontend && npx vitest run
