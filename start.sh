#!/usr/bin/env bash
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

BLUE=$'\033[34m'
GREEN=$'\033[32m'
YELLOW=$'\033[33m'
RESET=$'\033[0m'

if [ ! -f "$REPO_ROOT/.env" ] && [ ! -f "$REPO_ROOT/backend/.env" ]; then
    echo "${YELLOW}Error: No .env file found.${RESET}"
    echo "Run:  cp backend/.env.example backend/.env"
    echo "Then edit backend/.env with your settings."
    exit 1
fi

if [ ! -d "$REPO_ROOT/frontend/node_modules" ]; then
    echo "${YELLOW}Warning: frontend/node_modules not found. Running npm install...${RESET}"
    (cd "$REPO_ROOT/frontend" && npm install)
fi

cleanup() {
    echo ""
    echo "Stopping all processes..."
    kill 0
}
trap cleanup INT TERM EXIT

(
    cd "$REPO_ROOT/backend"
    uv run main.py 2>&1 | sed -u "s/^/${BLUE}[backend] ${RESET}/"
) &

sleep 2

(
    cd "$REPO_ROOT/frontend"
    npm run dev 2>&1 | sed -u "s/^/${GREEN}[frontend]${RESET} /"
) &

wait
