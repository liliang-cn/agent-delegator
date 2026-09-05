# agent-delegator

A small MCP (stdio) server that lets one agent hand a task to another agent CLI
installed on the same host. Built for [openclaw](https://github.com/openclaw/openclaw):
when a chat message says `@pi ...` or `@hermes ...`, the main agent calls
`call_pi` / `call_hermes` and relays the sub-agent's answer.

Sub-agents wired in by default:

| tool          | runs                                   | one-shot flag |
|---------------|----------------------------------------|---------------|
| `call_pi`     | [pi](https://github.com/earendil-works/pi) coding agent | `pi -p <prompt>` |
| `call_hermes` | [Hermes Agent](https://github.com/NousResearch/hermes-agent) | `hermes -z <prompt>` |

Arguments: `prompt` (required), `cwd` (default `/var/lib/openclaw/workspace`),
`timeout_seconds` (1–600, default 300). The result is JSON:
`agent`, `exit_code`, `duration_ms`, `output`, `stderr`, `timed_out`, `truncated`.

## Why not a ten-line Python script

The first version was one, and every `call_pi` timed out. Two things this
server does on purpose:

- **Child stdin is `/dev/null`.** `pi -p` blocks until EOF on a non-TTY stdin;
  inheriting the MCP JSON-RPC pipe means EOF never comes.
- **Timeouts kill the whole process group**, so a wedged node/python child does
  not outlive the request.

Tool descriptions are written so the model treats `@pi` / `@hermes` as a hard
instruction to call the tool and never fabricates a reply when the call fails.

## Build

```sh
go build -o agent-delegator .
# cross-compile for a Linux fleet
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o agent-delegator-arm64 .
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o agent-delegator-amd64 .
```

## Configure

Environment variables (all optional):

| variable                | default                          |
|-------------------------|----------------------------------|
| `DELEGATOR_PI_BIN`      | `/var/lib/openclaw/pi/pi`        |
| `DELEGATOR_HERMES_BIN`  | `/var/lib/openclaw/hermes/hermes`|
| `DELEGATOR_DEFAULT_CWD` | `/var/lib/openclaw/workspace`    |

Register with openclaw (request timeout must exceed the tool's own 600 s cap):

```sh
openclaw mcp add agent-delegator --command /usr/local/bin/agent-delegator --timeout 630
openclaw mcp reload
```

Any MCP client that speaks stdio works the same way.

## Verify it is really delegating

Ask for something the calling model cannot make up, then check the host:

```
@pi run `cat /etc/hostname; whoami` and reply with exactly those two lines
```

If the reply is your hostname and the service user, the sub-agent ran. A
plausible-looking answer with no tool call in the gateway log means the model
improvised.

## License

MIT
