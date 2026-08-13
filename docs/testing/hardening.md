# Hardening profile

`scripts/test-hardening.sh` runs the current-host concurrent packages with the race detector and repeated state/input/receipt tests. Release evidence records the exact command and host. Nightly infrastructure may extend fuzz/stress duration but may not replace deterministic regressions with retries.
