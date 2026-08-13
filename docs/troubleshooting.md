# Troubleshooting

- `unsafe state/runtime`: fix owner, symlink, file type, or group/world permissions; ShellBeam will not repair an ambiguous path automatically.
- `daemon_already_running`: use `shellbeam status`; do not delete a live socket. A stale socket is removed only after a connection probe fails.
- `daemon_unavailable`: start/install the daemon, then run doctor. Restarting `shellbeam mcp` does not stop daemon-owned commands.
- `operation_conflict`: the same operation ID was reused for different execution intent. Surface the conflict; do not create a replacement for an uncertain prior command.
- `cursor_out_of_range`: resume from the authoritative current end returned by the error.
- `input_gap`/`input_conflict`: use the authoritative `next_input_offset`. A retry must contain identical bytes at the same offset.
- `input_backpressure`: poll/wait and retry the identical write at the same offset.
- `abandoned/ambiguous`: the old daemon cannot prove the outcome. Inspect external state manually before deciding whether another operation is safe.
- service manager E2E script exit 3: that native user service manager is unavailable in the environment; this is `NOT_RUN`, not success.

