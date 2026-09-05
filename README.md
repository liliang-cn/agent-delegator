# agent-delegator

MCP server (stdio) that hands a task to another agent CLI on the same host.
Say `@pi ...` or `@hermes ...` to your main agent and it calls `call_pi` /
`call_hermes`, then relays the sub-agent's answer.

Built on the official [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk).

## Install

```sh
go install github.com/liliang-cn/agent-delegator@latest
```

Or build from source:

```sh
git clone https://github.com/liliang-cn/agent-delegator
cd agent-delegator
go build -o agent-delegator .
sudo install -m 0755 agent-delegator /usr/local/bin/
```

Cross-compile for a Linux box:

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o agent-delegator-amd64 .
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o agent-delegator-arm64 .
```

## Add it to your MCP client

openclaw:

```sh
openclaw mcp add agent-delegator --command /usr/local/bin/agent-delegator --timeout 630
openclaw mcp reload
```

Claude Code:

```sh
claude mcp add agent-delegator -- /usr/local/bin/agent-delegator
```

Any other client, in its JSON config:

```json
{
  "mcpServers": {
    "agent-delegator": {
      "command": "/usr/local/bin/agent-delegator",
      "env": {
        "DELEGATOR_PI_BIN": "/usr/local/bin/pi",
        "DELEGATOR_HERMES_BIN": "/usr/local/bin/hermes"
      }
    }
  }
}
```

Set the client's per-request timeout above 600 s, otherwise long tasks are cut
off by the client before the tool returns.

## Tools

| tool          | runs                     | agent |
|---------------|--------------------------|-------|
| `call_pi`     | `pi -p <prompt>`         | [pi](https://github.com/earendil-works/pi) |
| `call_hermes` | `hermes -z <prompt>`     | [Hermes Agent](https://github.com/NousResearch/hermes-agent) |

Arguments: `prompt` (required), `cwd`, `timeout_seconds` (1–600, default 300).
Returns JSON: `agent`, `exit_code`, `duration_ms`, `output`, `stderr`, `timed_out`.

## Environment

| variable                | default                           |
|-------------------------|-----------------------------------|
| `DELEGATOR_PI_BIN`      | `/var/lib/openclaw/pi/pi`         |
| `DELEGATOR_HERMES_BIN`  | `/var/lib/openclaw/hermes/hermes` |
| `DELEGATOR_DEFAULT_CWD` | `/var/lib/openclaw/workspace`     |

## License

MIT
