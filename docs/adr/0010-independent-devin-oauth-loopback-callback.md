---
status: superseded by ADR-0003
---

# Give each Devin OAuth login its own loopback callback listener

Devin required a plaintext `http://127.0.0.1:<port>/callback` redirect, which could not reliably use the main server when it bound elsewhere or enabled TLS. Give each login an ephemeral plaintext IPv4 loopback listener, validate the expected OAuth state against the pending session, and close it on success, failure, cancellation, or expiry.

Reuse management callback submission, status polling, cancellation, and credential persistence so remote browsers can submit their final callback URL without exposing a new public port or returning session tokens and PKCE verifiers to the frontend. The v8 sync migrated the fork's documentation to the v8 OAuth endpoints.

The merge initially added WebUI guidance, and the review fix isolated callback acquisition from the main listener. The reset removed this fork implementation; current OAuth behavior is determined by upstream, and historical v0 documentation is not a direction to modify deprecated endpoints.

## Evidence

[4b741bd3](https://github.com/Fryuni/CLIProxyAPI/commit/4b741bd368fafa9385ba437923f46841ca9d0029), [fb1c3e6b](https://github.com/Fryuni/CLIProxyAPI/commit/fb1c3e6bed0e6c455c61b502dbdaaa429ff03d5a), [001bbf30](https://github.com/Fryuni/CLIProxyAPI/commit/001bbf3066637ddfa58ffc1fd73f4c7a33747558), [125d3a4c](https://github.com/Fryuni/CLIProxyAPI/commit/125d3a4cb3cae258c1290ad6408e3ee1b1955c5e).
