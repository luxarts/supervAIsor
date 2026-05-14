.PHONY: up down poller poller-install logs test

up:
	docker compose -f infrastructure/docker-compose.yml up --build -d

down:
	docker compose -f infrastructure/docker-compose.yml down

logs:
	docker compose -f infrastructure/docker-compose.yml logs -f

poller:
	cd poller && go run ./cmd/poller

poller-install:
	cd poller && go build -o $(HOME)/.local/bin/supervaisor-poller ./cmd/poller
	@echo "Installed to $(HOME)/.local/bin/supervaisor-poller"
	@echo "Run: supervaisor-poller"

test:
	cd backend && go test ./...
	cd poller  && go test ./...
	cd frontend && npx vitest run
