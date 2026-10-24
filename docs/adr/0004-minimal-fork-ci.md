---
status: accepted
---

# Use a minimal CI workflow for the fork

Upstream automation includes releases, Docker publication, repository guards, PR retargeting, and model-catalog refresh machinery that the fork removed in favor of a single build-and-test workflow. Run `go build` and `go test ./...` on pushes to `main` and PRs targeting `main` from branches of the same repository, using the Go version in `go.mod` and read-only repository permissions.

The reset deliberately retained this CI policy, keeping verification separate from upstream's publication and contribution workflow. The same-repository PR condition is explicit in the implementation; the commit history does not state whether its motivation was cost, trust, or another concern, so this ADR does not assign one.

Upstream CI builds without running tests, so a sync can bring in tests that already fail on clean upstream. Reproduce each failure on clean upstream to separate it from merge regressions. Repair test-only defects in the fork, such as stale fixtures or races inside the test itself, and keep their assertions. Record runtime defects as local tickets instead of patching upstream runtime code or skipping the test; [ADR-0022](0022-sync-upstream-0d18d3b3.md) applies this to the first such sync, and [ADR-0024](0024-sync-upstream-911f49cb.md) to the next. A runtime defect that makes a test fail on every run is the exception, because it would keep fork CI red and hide real regressions. Patch it in the fork as a deliberate change with its own ADR, report it upstream, and drop the patch once upstream fixes it. [ADR-0026](0026-sync-upstream-8289ec4c.md) applies this to [ADR-0027](0027-interrupt-blocked-home-rebalance-rpcs.md).

## Evidence

[881c94a0](https://github.com/Fryuni/CLIProxyAPI/commit/881c94a036966392a553089a6a8a7659aca4dbf3), [2683879d](https://github.com/Fryuni/CLIProxyAPI/commit/2683879dceb2c3ec1c3544dffc476d6ef973e6c5), [125d3a4c](https://github.com/Fryuni/CLIProxyAPI/commit/125d3a4cb3cae258c1290ad6408e3ee1b1955c5e).
