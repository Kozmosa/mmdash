.PHONY: dev dev-prod setup test test-api coverage lint clean

dev:
	./scripts/start-all.sh --dev

dev-prod:
	./scripts/start-all.sh

setup:
	./scripts/setup.sh

test:
	cd backend && uv run pytest tests/ -v --tb=short

test-api:
	cd backend && uv run pytest tests/ -v --tb=short -m "requires_api"

coverage:
	cd backend && uv run pytest tests/ --cov=app --cov-report=term-missing

lint:
	cd backend && uv run ruff check app/ tests/
	cd frontend && npx tsc --noEmit --pretty

clean:
	@pkill -f "uvicorn" 2>/dev/null || true
	@pkill -f "next" 2>/dev/null || true
	@pkill -f "redis-server" 2>/dev/null || true
	@echo "Cleaned up lingering processes"
