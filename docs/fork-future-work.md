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
  The handler then restores the existing mode, so clients cannot clear
  cloaking through PUT.
- Fix: decode the raw `cloak` field per item to tell omission from an
  explicit `null`. Preserve the existing cloak only when the field is omitted.
  The fork previously carried this in `e42552a5`.

### Translate non-English test comments

- Where: `internal/runtime/executor/helps/stream_response_model_observer_test.go`
  (two comments).
- Problem: the repository requires English comments.
- Fix: replace `拆包` with "split chunks" and `合包` with "coalesced chunks".
  The fork previously carried this in `607b76cb`.

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
