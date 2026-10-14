---
status: accepted
---

# Treat closed-network HTTP 500 errors as transport failures

An HTTP 500 wrapping `use of closed network connection` describes a transient transport failure rather than evidence that the credential is unhealthy. Classify that message case-insensitively with existing transport lifecycle drops, permit the request's bounded retries, and avoid assigning a new credential cooldown for that failure.

Preserve already-active independent cooldowns; ordinary server-generated HTTP 500 errors still cool stored credentials and use the request-scoped bypass described in [ADR-0005](0005-request-scoped-http-500-retry-rounds.md). The original change was reset and then deliberately reapplied, and the retry/exhaustion tests distinguish the two failure classes.

## Upstream implementation notes

Upstream already matched `use of closed network connection` in transport messages that carry no HTTP status. `56eee6a4`, merged by [ADR-0022](0022-sync-upstream-0d18d3b3.md), adds HTTP/2 closed-connection messages to both the lifecycle and transient-transport classifications. Upstream still keeps every HTTP-status error on the credential path, so the HTTP 500 wrapping rule remains fork-only. The sync dropped the fork's duplicate closed-network case from `isTransientTransportMessage` in favor of upstream's existing match; behavior is unchanged.

## Evidence

[72b82af1](https://github.com/Fryuni/CLIProxyAPI/commit/72b82af1d3753b09bba789f4463fac2af514ec38), [bd312a1f](https://github.com/Fryuni/CLIProxyAPI/commit/bd312a1f1d3d89de1e7bb0bb9ca483b6ef6477a0), [b13568f6](https://github.com/Fryuni/CLIProxyAPI/commit/b13568f651fc0b9fd6f9d1904ce75255473b90dd), [5ac127ac](https://github.com/Fryuni/CLIProxyAPI/commit/5ac127ac12f2fe7c6e04c15286dfa32c54a1b3a8), [0c7dd79d](https://github.com/Fryuni/CLIProxyAPI/commit/0c7dd79de2200742e765b7a30b66207a4e4f2772), [a0135cde](https://github.com/Fryuni/CLIProxyAPI/commit/a0135cde35f77c757b59f3c5a4b995062293be3a).
