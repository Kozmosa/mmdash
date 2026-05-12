# Needed Improvements

This file tracks known UX and design issues that are intentionally not fixed in the current stage.

## Experiment Flow

- The experiment page still requires manual Git repository path entry even when the current project already has a related repository context.
- Parameter extraction is based on static assignment scanning, while execution injects values through environment variables. This is workable but still semantically leaky for users.
- The experiment history view can show raw internal file structure details that are useful for debugging but not yet optimized for everyday users.

## Authentication And Routing

- Main application routes still depend on client-side auth restoration. The current fix reduces misleading rendering, but there is still no full server-side route protection model.

## Testing Infrastructure

- The repository still lacks a first-class browser E2E framework and stable fixtures for UI-level regression testing.
- Current validation coverage is strong for API and local-agent behavior, but UI interaction regressions still require extra setup.
