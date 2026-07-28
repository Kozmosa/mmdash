# Needed Improvements Backlog

This backlog tracks technical debt and UX gaps that are not fully addressed yet.

## Debt Items

### DEBT-001
- Title: Experiment page still requires manual Git repository path entry
- Severity: Medium
- Area: Experiment Flow
- Current behavior: Users must manually paste a local repository path even when the current project already implies a repository context.
- Impact: Adds repeated setup friction and makes experiment execution feel disconnected from project setup.
- Suggested next action: Introduce project-level repository context or saved local path binding and prefill it on the experiment page.
- Status: Open

### DEBT-002
- Title: Solver parameter extraction and execution semantics are still mismatched
- Severity: Medium
- Area: Experiment Flow
- Current behavior: Parameter extraction scans static Python assignments, while experiment execution injects values through environment variables.
- Impact: Users may assume the platform understands solver interfaces more deeply than it actually does, leading to confusion.
- Suggested next action: Make the execution model explicit in UI and gradually evolve extraction toward a declared parameter contract.
- Status: Open

### DEBT-003
- Title: Experiment history view is still too low-level for everyday users
- Severity: Low
- Area: Experiment Flow
- Current behavior: The history panel exposes directory-structure-oriented artifacts directly.
- Impact: Good for debugging, but not ideal for product-level readability.
- Suggested next action: Add a higher-level presentation layer on top of raw logs, params, and generated files.
- Status: Open

### DEBT-004
- Title: Main-route access control needs server-side enforcement
- Severity: Medium
- Area: Authentication And Routing
- Current behavior: Main app access was previously client-led; this cycle moves to cookie-backed server redirect, but it is still not a full server-owned auth model.
- Impact: Better than pure client gating, but still not equivalent to a complete session-based server auth architecture.
- Suggested next action: Evaluate a stronger server-auth pattern with clearer token lifecycle and server-trusted user resolution.
- Status: In Progress

### DEBT-005
- Title: Browser E2E needs to become a first-class maintained test layer
- Severity: Medium
- Area: Testing Infrastructure
- Current behavior: This cycle introduces Playwright and initial coverage, but the suite is still minimal and not yet wired into a broader regression discipline.
- Impact: UI regressions can still escape if new flows are not added systematically.
- Suggested next action: Expand Playwright coverage to key authenticated flows and define a standard local/CI execution contract.
- Status: In Progress

### DEBT-006
- Title: Technical debt backlog lacked structure and lifecycle tracking
- Severity: Low
- Area: Documentation
- Current behavior: Prior debt notes were free-form summaries without consistent severity, impact, or ownership cues.
- Impact: Harder to prioritize, review, and retire debt items over time.
- Suggested next action: Keep this document updated as the single structured debt backlog and mark items `Open`, `In Progress`, or `Resolved`.
- Status: Resolved
