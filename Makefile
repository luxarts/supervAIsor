.PHONY: up down clean poller poller-build poller-install logs test

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
	cd poller && go build -o supervaisor ./cmd/poller
	@echo "Built poller/supervaisor"

poller-install:
	cd poller && go build -o $(HOME)/.local/bin/supervaisor-poller ./cmd/poller
	@echo "Installed to $(HOME)/.local/bin/supervaisor-poller"
	@echo "Run: supervaisor-poller"

test:
	cd backend && go test ./...
	cd poller  && go test ./...
	cd frontend && npx vitest run
