# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**supervAIsor** is an AI agent supervision platform with a game-like interface (inspired by RTS/ARPG games like Age of Empires). It integrates with Claude Code to visually monitor and manage multiple AI agents working on a project.

## Planned Architecture

The project is split into three top-level directories:

- `backend/` — Go-based API server
- `frontend/` — React (Node.js) web UI
- `infrastructure/` — Dockerfiles, Docker Compose, and deployment configuration

### Key Concepts

- **Agents**: Represent Claude Code instances working on a project. Agents have types (skill sets) and are managed visually like NPCs.
- **World**: A project-based virtual environment with rooms, workstations, and infrastructure.
- **Agent Types**: Predefined skill configurations used to create agents faster.

## Development Commands

> These commands will be defined as the project is built out. Update this section as `backend/`, `frontend/`, and `infrastructure/` are scaffolded.

### Backend (Go)
```bash
# From backend/
go run .          # Run the server
go test ./...     # Run all tests
go build -o bin/supervaisor .  # Build binary
```

### Frontend (Node.js/React)
```bash
# From frontend/
npm install       # Install dependencies
npm run dev       # Start dev server
npm run build     # Production build
npm test          # Run tests
```

### Infrastructure
```bash
docker compose up        # Start all services
docker compose down      # Stop all services
```

## Implementation Notes

- The UI design is intentionally game-like — prioritize real-time feedback, visual agent status, and interactive agent management.
- Backend should expose a WebSocket or SSE endpoint for live agent status updates to the frontend.
- Claude Code integration: agents are represented by Claude Code processes; the platform tracks their state and output.
