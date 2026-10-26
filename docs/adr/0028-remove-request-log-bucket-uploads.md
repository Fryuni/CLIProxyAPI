---
status: accepted
---

# Remove request log bucket uploads

[ADR-0025](0025-upload-request-logs-to-a-bucket.md) added `observability.logs.request-log-bucket`, which uploaded completed request logs to a `gocloud.dev` bucket. After running it for real, shipping these files off the instance is better done by the infrastructure layer, which can add compression and cross-machine block deduplication to the remote copies. The fork reverts PR #15 (`0deb4f86`): the setting, the uploader, the temp-file-then-rename writes, and the startup cleanup are gone, and request logging matches upstream again. `go.mod` and `go.sum` return to upstream `8289ec4c`'s versions, dropping `gocloud.dev`, its transitive modules, and the higher minimums ADR-0026 kept for it. A config that still sets `request-log-bucket` loads, but the key is ignored and logs stay local.
