# Box contracts

The stable Core-to-Box boundary is implemented in `types.go` and
`validate.go`. It covers the frozen `run_spec`, capability/runtime/resource
advertisement, lease task, bounded log, manifest, and `artifact.zip` pointer.
The authoritative JSON/OpenAPI contracts remain under `contracts/`; this Go
package is the Box-side wire representation and validation layer.
