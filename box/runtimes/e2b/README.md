# E2B runtime

E2B is a provider-neutral adapter over the same Sandbox runtime interface.
Provider-specific session IDs and credentials stay behind the adapter and do
not enter Core task contracts. Offline conformance covers run, cancel, and
destroy; real provider acceptance remains an external credentialed deployment
step and is intentionally not represented as passed by this branch.
