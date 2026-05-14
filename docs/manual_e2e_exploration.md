# Manual E2E Exploration Script

## Goal

Use this script after automated checks to surface flow discontinuity, usability friction, and resilience issues that are hard to encode as binary assertions.

## Core Paths

1. Register, create team, create project with `local_file`.
2. Upload a small PDF, confirm the file appears, download it, then delete it.
3. Move to `/model`, create a model document, write a small draft, run symbol/structure/error/formula analysis.
4. Move to `/experiment` and observe whether the project context naturally carries over.
5. Connect Local Agent, input a repo path, scan solver files, extract params, run a small experiment.
6. Open history, inspect result completeness, then return to the run tab and retry.
7. Optionally open `/timeline` and add key milestones for the same story.

## What To Observe

- Continuity:
  - After PDF upload, is the next step obvious?
  - After landing in the model page, does the product help the user start writing without re-orienting?
  - Does the experiment page reuse project context, or require repeated setup?
  - Is the relationship between project, repo, solver, and params understandable without explanation?
- Error recovery:
  - Upload invalid file, then immediately retry a valid file.
  - Save model content, refresh, and confirm the draft is still there.
  - Use an invalid repo path, then correct it.
  - Stop Local Agent or refresh the page mid-flow and reconnect.
- Semantics:
  - Do AI analysis outputs feel tied to the current document, or detached from user intent?
  - Does “parameter extraction” feel consistent with “environment variable injection”?
  - Can a user infer that solver source is not being rewritten?
- Stability:
  - Double-click upload / scan / run actions.
  - Trigger multiple AI analysis tools in sequence.
  - Switch tabs during experiment execution.
  - Refresh after a finished run and verify history still works.

## Suggested Defect Tags

- `flow-gap`
- `copy-unclear`
- `state-loss`
- `retry-poor`
- `model-to-exp-gap`
- `agent-reconnect`
- `history-too-low-level`
- `param-model-mismatch`
