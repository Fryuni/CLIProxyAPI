# Fork Future Work

This fork tracks upstream (`router-for-me/CLIProxyAPI`) and carries only two
fork-specific changes: the minimal CI workflow (`.github/workflows/test.yml`)
and HTTP 500 retries within request retry rounds (`sdk/cliproxy/auth`).

Everything else matches upstream on purpose, to keep upstream syncs cheap. The
items below came up in review but were left out of the upstream reset to avoid
diverging from upstream again. Prefer fixing them upstream; re-land them in the
fork only if upstream declines and the fix is worth the sync cost.

## Fork fixes dropped by the upstream reset

The fork used to carry these fixes. The reset restored upstream's behavior.

### Bound zstd decoder memory when logging request bodies

- Where: `decodeCapturedZstdRequestBodyWithLimit` in
  `internal/api/middleware/request_logging.go`.
- Problem: the decoder uses the library's default memory limits.
  `io.LimitReader` only caps decoded output. A small frame that declares a
  large window can still force a large allocation before the limit applies.
- Fix: pass `zstd.WithDecoderConcurrency(1)` and
  `zstd.WithDecoderMaxMemory(uint64(max(limit, 1<<20)))` to `zstd.NewReader`.
  The fork previously carried this in `15a7ad58`.
- Priority: high. This is a memory-exhaustion vector on request logging.

### Let `PUT claude-api-key` clear cloaking with an explicit `null`

- Where: `PutClaudeKeys` in `internal/api/handlers/management/config_lists.go`.
- Problem: an omitted `cloak` and `"cloak": null` both decode to a nil pointer.
  For both, the handler rebuilds a cloak that holds only the old `mode`. That
  means clients cannot clear cloaking through PUT, and omitting the field
  silently drops the other cloak settings (`strict-mode`, `sensitive-words`,
  `cache-user-id`).
- Fix: decode the raw `cloak` field per item to tell omission from an
  explicit `null`. When the field is omitted, clone the complete existing cloak
  config. When it is `null`, leave cloaking cleared. The fork previously
  carried this in `e42552a5`.

### Translate non-English test comments

- Where: `internal/runtime/executor/helps/stream_response_model_observer_test.go`
  (two comments).
- Problem: the repository requires English comments.
- Fix: replace `拆包` with "split chunks" and `合包` with "coalesced chunks".
  The fork previously carried this in `607b76cb`.

## HTTP 500 retry follow-ups

### Tighten the HTTP 500 retry-round bypass guard

- Where: `recordAttemptedAuthResult` / `requestRetryAttempt` and
  `http500RetryRoundCandidate` in `sdk/cliproxy/auth`, plus `MarkResult` in
  `conductor_cooldown.go`.
- Background: a request that got HTTP 500 may retry the same credential in its
  next retry round. Selection uses a request-scoped clone that clears the
  cooldown the 500 set. The stored credential stays cooled for other requests.
  The guard is coarse, which leaves these concurrency gaps:
  - **Unrelated results suppress the bypass.** The guard requires the auth-wide
    `Generation` to match the recorded value. Every `MarkResult` bumps it, so a
    concurrent result for another model on the same credential disables the
    bypass. This fails safe (the request falls back to the regular cooldown
    wait), but busy credentials retry immediately less often than intended.
  - **Overlapping cooldowns are cleared together.** If a concurrent request
    records a 503 and this request's 500 lands just after it with a later
    deadline, the 500 is treated as the only cause of the cooldown. The bypass
    then clears the whole deadline, including the still-active 503 cooldown.
  - **Stale results after re-registration.** `MarkResult` resolves results by
    auth ID only (upstream behavior), and the attempt records the epoch it
    reads after the update. A request that selected registration epoch 1 and
    finishes after the ID is re-registered (epoch 2) therefore marks epoch 2,
    and its next round can bypass epoch 2's cooldown.
- Impact: every gap is request-scoped and at worst causes one extra upstream
  attempt against a credential that is cooling down for another reason.
  Stored state and other requests are unaffected.
- Fix: carry the selected credential's `RegistrationEpoch` through execution
  into `Result`, and drop or ignore stale results in `MarkResult`. Record in the
  attempt the model-state deadline this result set, plus any foreign cooldown
  already active before it. Bypass only while the stored model state still
  carries that deadline, and restore the foreign deadline instead of clearing
  to zero. Keep the auth-wide check only for auth-scoped results. Add tests for
  each gap: a concurrent other-model result, a 503 then a 500, and a stale
  result after re-registration.

## Upstream behavior flagged in review

These are upstream design choices, not fork changes. They are recorded so
reviewers can see they were considered. Raise them upstream if they matter.

- **Short request-log filename IDs**
  (`internal/logging/request_logger_writer.go`, upstream `d33f63f8`): only the
  random suffix of the UUIDv7 request ID is kept in log filenames and ID
  lookups. Suffix collisions reach about 50% odds at roughly 77k logged
  requests.
- **`apply-patch` capability default docs** (`internal/config/sdk_config.go`,
  upstream `63c04b4b`): the documented default says `false` clears template
  metadata. In practice, a nil capability callback keeps `freeform` from the
  built-in templates.
- **V8 normalization on deprecated v0 writes**
  (`internal/api/handlers/management/config_basic.go`, upstream `3be5fa44`):
  `WriteConfig` normalizes V8-layout YAML for both `/v8` and the deprecated
  `/v0` `PUT config.yaml` routes.
- **Devin OAuth callback with TLS enabled**
  (`internal/api/handlers/management/auth_files_devin_oauth.go`, upstream
  `cca35aee`): the redirect URI is always plain `http://` loopback on the main
  server port. When `tls.enable` is set, that port only serves HTTPS, so Devin
  OAuth cannot complete. A dedicated plaintext loopback callback listener would
  fix it. Flagged as high severity.
- **Devin catalog refresh does not notify listeners**
  (`internal/registry/devin_models_updater.go`): a periodic Devin catalog
  update does not invoke the model-refresh callback. Registered models, and
  therefore `/models` and routing, stay on the old catalog until the next
  reload.
- **Devin catalog fetch timeout** (`fetchDevinModelsFromRemote` in
  `internal/registry/devin_models_updater.go`): the client and request
  deadlines stay active while the catalog body is read. AGENTS.md limits
  timeouts to credential acquisition and the listed exceptions, and model
  catalog refresh is not one of them.
- **`TraceID` contract after the UUIDv7 request-ID change**
  (`internal/logging/requestid.go`): request IDs are now full UUIDv7 strings
  and flow into usage `TraceID`. The public docs on `usage.Record.TraceID`
  (`sdk/cliproxy/usage/manager.go`) and `pluginapi.UsageRecord.TraceID`
  (`sdk/pluginapi/types.go`) still say "8-character hex".
- **Alias-specific `use-max-completion-tokens`**
  (`ShouldUseMaxCompletionTokensForModel` in
  `internal/runtime/executor/helps/openai_compat_max_tokens.go`): the upstream
  model name is matched before the requested alias, and the first matching
  entry wins. When several aliases share one upstream model, the first entry's
  setting applies to all of them and overrides alias-specific values.
- **Failed Interactions streams end with success events**
  (`internal/translator/interactions/claude` and
  `internal/translator/claude/interactions`): Interactions stream failures can
  be translated into Claude terminal events that signal success. Per
  AGENTS.md, translator fixes should land alongside broader changes or
  upstream.
