# agent-delegator

MCP server that runs a task with another agent CLI on the same host.
Say `@pi ...` or `@hermes ...` and the main agent calls `call_pi` / `call_hermes`.

## Install

```sh
go install github.com/liliang-cn/agent-delegator@latest
```

## Use

openclaw:

```sh
openclaw mcp add agent-delegator --command agent-delegator --timeout 630
openclaw mcp reload
```

Claude Code:

```sh
claude mcp add agent-delegator -- agent-delegator
```

Other clients:

```json
{
  "mcpServers": {
    "agent-delegator": { "command": "agent-delegator" }
  }
}
```

## Tools

| tool          | runs               |
|---------------|--------------------|
| `call_pi`     | `pi -p <prompt>`   |
| `call_hermes` | `hermes -z <prompt>` |

Arguments: `prompt` (required), `cwd`, `timeout_seconds` (default 300, max 600).

## Environment

| variable                | default                           |
|-------------------------|-----------------------------------|
| `DELEGATOR_PI_BIN`      | `/var/lib/openclaw/pi/pi`         |
| `DELEGATOR_HERMES_BIN`  | `/var/lib/openclaw/hermes/hermes` |
| `DELEGATOR_DEFAULT_CWD` | `/var/lib/openclaw/workspace`     |

## License

MIT
