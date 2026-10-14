---
status: accepted
---

# Reset to upstream and keep a small deliberate fork delta

The fork had accumulated transcription support, Devin fixes, request limits, and merge-review corrections across repeated upstream syncs. On October 4, `125d3a4c` replaced its tree with upstream `d7914afd` except for fork CI, and `a0135cde` then reapplied HTTP 500 retry rounds; `docs/fork-future-work.md` explicitly explains that the other changes were left out to keep upstream syncs cheap.

Retain CI and bounded retry behavior as intentional fork differences, and record other discoveries for evaluation upstream or later local work instead of automatically restoring them. This supersedes the earlier preservation policy and the fork-specific implementations recorded in the historical ADRs; some equivalent behavior can still exist independently in upstream. The historical reset is evidence of that decision, not an instruction to reset this workspace or discard the new local tracker and ADR documentation.

## Evidence

[125d3a4c](https://github.com/Fryuni/CLIProxyAPI/commit/125d3a4cb3cae258c1290ad6408e3ee1b1955c5e), [a0135cde](https://github.com/Fryuni/CLIProxyAPI/commit/a0135cde35f77c757b59f3c5a4b995062293be3a), [6b763383](https://github.com/Fryuni/CLIProxyAPI/commit/6b763383c43ac26af5890e3b1f32717598812b8d), [75f62ac5](https://github.com/Fryuni/CLIProxyAPI/commit/75f62ac565d46737a76bd898e1b4d4f1061f76aa).
