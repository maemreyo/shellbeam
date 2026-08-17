# Rich Local Media Phase A Evidence

## Verdict

```text
phase_a_status = NOT_RUN
phase_a_pass = false
reason = complete frozen host-compatibility matrix is not yet evidenced
```

This receipt normalizes only evidence actually observed. Archived tracer scorecards remain `27/27 NOT_RUN`; successful ChatGPT replies are treated as provenance inputs, not as permission to rewrite those archived files.

## Frozen execution context

- Topology: one MCP tool, `local_shell`.
- Current raw maximum candidate: 7 MiB = 7,340,032 bytes.
- Historical 8 MiB live-tunnel candidate: `FAIL / transport-ceiling` from a runtime/conversation observation associated with the run-2 artifact set; the exact HTTP 413 body is not retained in the current local `.build` logs, so this receipt does not invent a local-log citation.
- Earlier timed-out/stale-address 8 MiB attempt: `NOT_RUN`.
- 7 MiB maximum-payload visible-token result: PASS 3/3 in fresh ChatGPT trials, privately matched against the current-run manifest without recording token values here.
- Direct workspace A visible-token result: one observed PASS trial, privately matched against the run-1 manifest.
- Direct workspace B visible-token result: one observed PASS trial, privately matched against the run-1 manifest.
- Current-session native `ImageContent` vision probes independently passed PNG (`formats/probe.png`), JPEG (`formats/probe.jpg`), and WebP (`formats/probe.webp`); each visible code, exact `display_address`, byte count, and MIME privately matched the current 7 MiB manifest.
- Current-session payload probes at 64 KiB, 256 KiB, 1 MiB, and 4 MiB each passed visible-code vision with exact bytes/MIME privately matched to the current manifest. Section 21.4 requires three fresh conversations only for the maximum case, so these four 1/1 payload rows are complete.
- Current-session CWD-form PNG (`cwd=/phase-a/synthetic`, `path=artifacts/cwd-settings.png`) also passed exact visible-code/address/bytes/MIME correlation, but it is recorded only as supplemental evidence because the `direct-cwd` prompt-class row requires three fresh conversations and this conversation is not fresh.

## Immutable local artifact provenance

| Artifact set | File | SHA-256 |
|---|---|---|
| local-preflight | manifest.json | `c47fa093cffc34e805ea1f22be816b5e8cdb399eee7949c3e413127081d9bf73` |
| local-preflight | goldens.json | `33e5492ddc282c30a4b5c3becd0a42fc052bd4f2afaeb7cc011ee1ba1e5f37b0` |
| local-preflight | scorecard.json | `99e17224bb1fd25eabeb65d855ad4ba2c96585b639d504a45852175d7e6d41e3` |
| local-preflight | run-info.json | `98fd5a36e160fc536f9cb5c7ce604e35c57b65af726510e88c51d358240fa2af` |
| real-host-run1 | manifest.json | `95480332855d4f907b0a1d201f7ccaee060a0193dd9a313c3d2989e2bbb14618` |
| real-host-run1 | goldens.json | `60a174422686d62dbbe02693bd789cb5156ba53d6524c2e7652a936715ecd314` |
| real-host-run1 | scorecard.json | `5ba9155ddd9e5bf6561d2eb8a618af9d25066354889f7d97527c24075c03cbf8` |
| real-host-run1 | run-info.json | `98fd5a36e160fc536f9cb5c7ce604e35c57b65af726510e88c51d358240fa2af` |
| real-host-run2-413 | manifest.json | `6070f3a5c0af8f75d78c05a48015e55d99481f306d3deb8dd7d1675c3815eeb0` |
| real-host-run2-413 | goldens.json | `4cce7258f84a5dd456ee7c2f1f40b229cfa96003fb83db74dfeb3da674bfe8f9` |
| real-host-run2-413 | scorecard.json | `e7df1b26a8abc965d5764156edd4b6a53b86e23858cf84a8a38f10749ef02132` |
| real-host-run2-413 | run-info.json | `98fd5a36e160fc536f9cb5c7ce604e35c57b65af726510e88c51d358240fa2af` |
| real-host-7m | manifest.json | `81db5aedc664a2ef5c1d54843c4a39a6b3b5444c2e975364287e76e39cacb931` |
| real-host-7m | goldens.json | `91a1c44878b013f9866eaff7ff9035f9e5a09122f4ca57bc6533ffa074cb9f24` |
| real-host-7m | scorecard.json | `ad79cba8f8fdabd2b0da73b9e7460ff9cc79730354036614f3906183fe2cadf7` |
| real-host-7m | run-info.json | `98fd5a36e160fc536f9cb5c7ce604e35c57b65af726510e88c51d358240fa2af` |

## Normalized hard-gate matrix

`PASS/FAIL/NOT_RUN` below is derived from observed trial counts. Missing trials remain `NOT_RUN`; they are not inferred from neighboring success.

| Check | Required / threshold | Pass | Fail | Not run | Status | Provenance |
|---|---:|---:|---:|---:|---|---|
| direct-workspace-a | 3 / 3 | 1 | 0 | 2 | NOT_RUN | run-1 manifest + one private token match |
| direct-workspace-b | 3 / 3 | 1 | 0 | 2 | NOT_RUN | run-1 manifest + one private token match |
| direct-cwd | 3 / 3 | 0 | 0 | 3 | NOT_RUN | same-session CWD ImageContent vision/correlation PASS exists, but is not counted because this prompt-class row requires fresh conversations |
| indirect-image-goal | 3 / 2 | 0 | 0 | 3 | NOT_RUN | frozen golden only |
| established-followup | 3 / 2 | 0 | 0 | 3 | NOT_RUN | frozen golden only |
| negative-no-media | 3 / 3 | 0 | 0 | 3 | NOT_RUN | frozen golden only |
| unsupported-pdf | 3 / 3 | 0 | 0 | 3 | NOT_RUN | frozen golden only |
| sensitive-unestablished | 3 / 3 | 0 | 0 | 3 | NOT_RUN | frozen golden only |
| payload-64k | 1 / 1 | 1 | 0 | 0 | PASS | current-session native ImageContent visible-code + exact 65,536 bytes/MIME private manifest match |
| payload-256k | 1 / 1 | 1 | 0 | 0 | PASS | current-session native ImageContent visible-code + exact 262,144 bytes/MIME private manifest match |
| payload-1m | 1 / 1 | 1 | 0 | 0 | PASS | current-session native ImageContent visible-code + exact 1,048,576 bytes/MIME private manifest match |
| payload-4m | 1 / 1 | 1 | 0 | 0 | PASS | current-session native ImageContent visible-code + exact 4,194,304 bytes/MIME private manifest match |
| max-payload-7m | 3 / 3 | 3 | 0 | 0 | PASS | current 7 MiB manifest + three private token matches |
| format-png | 1 / 1 | 1 | 0 | 0 | PASS | explicit current-session PNG probe + current 7 MiB manifest private token/address/bytes/MIME match |
| format-jpeg | 1 / 1 | 1 | 0 | 0 | PASS | current-session native JPEG ImageContent visible-code + exact address/3,936 bytes/MIME private manifest match |
| format-webp | 1 / 1 | 1 | 0 | 0 | PASS | current-session native WebP ImageContent visible-code + exact address/186 bytes/MIME private manifest match |
| address-collision-a | 3 / 3 | 1 | 0 | 2 | NOT_RUN | workspace A one exact private token match |
| address-collision-b | 3 / 3 | 1 | 0 | 2 | NOT_RUN | workspace B one exact private token match |
| disclosure-confirmation | 1 / 1 | 0 | 0 | 1 | NOT_RUN | confirmation UI/address display not recorded |
| production-disclosure | 1 / 1 | 0 | 0 | 1 | NOT_RUN | production disclosure timing not run |
| annotation-omitted | 1 / 1 | 0 | 0 | 1 | NOT_RUN | audience variant matrix not run |
| annotation-user-assistant | 1 / 1 | 0 | 0 | 1 | NOT_RUN | audience variant matrix not run |
| annotation-selection | 1 / 1 | 0 | 0 | 1 | NOT_RUN | neither complete variant matrix is available |
| remembered-approval | 1 / 1 | 0 | 0 | 1 | NOT_RUN | host remembered-approval availability not established |

## Historical rejected maximum candidate

The former 8 MiB candidate is not a current hard-gate row. The valid live-tunnel request was observed to fail at the Secure MCP Tunnel body ceiling after base64/envelope expansion. This is recorded as historical `FAIL / transport-ceiling`; it does not overwrite the current 7 MiB row. The retained run-2 artifacts establish run provenance, but the exact 413 body line is no longer present in local retained logs.

## Derived decision

At least one mandatory Phase A row is incomplete, including fresh direct/CWD/collision trials, indirect/follow-up selection trials, negative/unsupported/sensitive cases, disclosure/confirmation, and both annotation audience variants. Therefore:

```text
phase_a_pass = false
phase_a_status = NOT_RUN
```

No production implementation may be unlocked from this receipt alone.
