# Local Docker runtime

Local Docker runs the fixed Sandbox argv in a predefined image with a
read-only workspace, a bounded output mount, dropped capabilities, resource
limits, and no network unless explicitly enabled. The runtime never receives
an arbitrary shell string.
