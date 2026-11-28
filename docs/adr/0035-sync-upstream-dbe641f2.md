---
status: accepted
---

# Sync upstream dbe641f2 while keeping the fork delta

This merge brings 319 upstream commits (310 excluding merges), from `f5c5681a` to `dbe641f2`, into fork main at `aa5bcc9d`, its first parent. The fork keeps:

- its CI ([ADR-0004](0004-minimal-fork-ci.md)). Upstream's modified `pr-test-build.yml` stays deleted.
- its HTTP 500 retry-round behavior ([ADR-0005](0005-request-scoped-http-500-retry-rounds.md), [ADR-0006](0006-closed-network-errors-are-transport-failures.md), [ADR-0007](0007-select-prepared-candidates-for-delegated-strategies.md))
- its quota cache ([ADR-0023](0023-serve-plugin-quotas-from-a-background-refreshed-cache.md))

Everything else is upstream's, following [ADR-0003](0003-minimize-fork-divergence.md). Upstream now implements two fork features, so the fork drops its own implementations:

- **GitHub Copilot ([ADR-0032](0032-serve-github-copilot-subscriptions-natively.md)):** upstream `7ae771fc` and later commits add a native `copilot` provider. Its `internal/auth/copilot` package occupies the same directory as the fork's. The fork removes its `github-copilot` provider and returns every file the provider touched to upstream's version. Existing `github-copilot` credentials must log in again with `--copilot-login`.
- **Antigravity grouped models ([ADR-0034](0034-unify-antigravity-grouped-models-by-reasoning-effort.md)):** upstream `2f86992c` routes each grouped model by its requested thinking level. Every file touched by the fork's version returns to upstream's version, including the five conflicted Antigravity executor files, `payload_finalizer.go`, both `antigravity_models.go` files, and `service_models.go`. Upstream publishes suffixed catalog IDs instead of the fork's bare group IDs.

Both ADRs are superseded and carry upstream implementation notes. Before the two features, the files they touched carried no other fork delta: each one matches upstream at the fork's previous base.

Conflicts in the retained delta and their resolutions:

- **`conductor.go`:** upstream changes `modelPoolOffsets` to a per-auth map and wraps the hook in `hookChain`. The fork keeps both and also keeps its `pluginBuiltinMixedOffsets` field.
- **`conductor_cooldown.go`:** upstream `applyAuthFailureState` no longer skips cooldown for TOS-violation errors. The fork keeps that condition and still returns the third value, which reports whether the failure installed the credential cooldown.

Upstream `69607634` fixes the [config-write atomicity ticket](../../.scratch/config-write-atomicity/issues/01-write-config-atomically.md) with a lock instead of the proposed temporary file and rename. Every config read and write now runs under one process-wide lock and an OS file lock, so a reload never reads a partially written file. The ticket is resolved. Every other open ticket was checked against the upstream diff and is unchanged. The Claude prompt-cache ticket and the reasoning-effort ordering ticket remain partly addressed, as before.

## Evidence

On the merged tree, `go build ./...`, `go vet` of the conflicted packages, and `go test ./...` pass.
