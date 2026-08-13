# Security model

ShellBeam deliberately executes as the current OS user with that user's full authority. It does not sandbox commands, blacklist dangerous syntax, contain network access, or prevent commands from changing local/remote systems. The MCP tool is marked destructive and open-world.

The remote identity/transport boundary belongs to OpenAI Secure MCP Tunnel. Locally, the MCP bridge has no process authority and reaches the daemon only through an owner-only Unix socket. The daemon checks peer UID before request decoding. State and runtime paths reject unsafe type/mode/owner conditions and operation reservations are durable before spawn.

Commands, cwd, environment, stdin, output, credentials, tokens, raw paths, and arbitrary OS errors are excluded from default operator logs. Command output remains in its separately quota-bound session log. Input ledgers store offsets, lengths, kinds and hashes, not stdin contents.

V1 cannot reattach after daemon failure. A new daemon marks unresolved sessions `abandoned/ambiguous` and never claims ownership using a PID/PGID loaded from disk. Processes intentionally escaping their process group and external side effects remain documented limitations.

Report vulnerabilities privately to the project owner. Do not include real credentials, commands, or captured output in a report unless required and safely redacted.

