# agent-delegator

MCP server that runs a task with another agent CLI on the same host.
Say `@pi ...`, `@codex ...`, `@hermes ...` and the main agent calls `call_pi`,
`call_codex`, `call_hermes`. Any agent CLI works: declare it once in the config.

## Install

```sh
go install github.com/liliang-cn/agent-delegator@latest
```

## Configure agents

`~/.config/agent-delegator/agents.json` (or `DELEGATOR_CONFIG=<path>`).
`{prompt}` marks where the task goes; without it the prompt is appended.

```json
{
  "agents": [
    { "name": "pi",       "command": "pi",       "args": ["-p", "{prompt}"] },
    { "name": "hermes",   "command": "hermes",   "args": ["-z", "{prompt}"] },
    { "name": "claude",   "command": "claude",   "args": ["-p", "{prompt}"] },
    { "name": "codex",    "command": "codex",    "args": ["exec", "--skip-git-repo-check", "{prompt}"] },
    { "name": "gemini",   "command": "gemini",   "args": ["-p", "{prompt}"], "env": { "GEMINI_CLI_TRUST_WORKSPACE": "true" } },
    { "name": "agy",      "command": "agy",      "args": ["-p", "{prompt}"] },
    { "name": "opencode", "command": "opencode", "args": ["run", "{prompt}"] },
    { "name": "kimi",     "command": "kimi",     "args": ["--quiet", "-p", "{prompt}"] },
    { "name": "qwen",     "command": "qwen",     "args": ["-p", "{prompt}"] },
    { "name": "mimo",     "command": "mimo",     "args": ["run", "{prompt}"] },
    { "name": "zcode",    "command": "zcode",    "args": ["--print", "--prompt", "{prompt}"] },
    { "name": "droid",    "command": "droid",    "args": ["exec", "{prompt}"] },
    { "name": "crush",    "command": "crush",    "args": ["run", "-q", "{prompt}"] },
    { "name": "goose",    "command": "goose",    "args": ["run", "-t", "{prompt}"] },
    { "name": "amp",      "command": "amp",      "args": ["-x", "{prompt}"] },
    { "name": "copilot",  "command": "copilot",  "args": ["-s", "-p", "{prompt}"] },
    { "name": "auggie",   "command": "auggie",   "args": ["--print", "--quiet", "{prompt}"] },
    { "name": "aider",    "command": "aider",    "args": ["--message", "{prompt}", "--yes-always"] }
  ]
}
```

Or per agent in the environment, no file needed:

```sh
DELEGATOR_AGENT_PI="/opt/pi/pi -p {prompt}"
DELEGATOR_AGENT_HERMES="hermes -z {prompt}"
```

`agent-delegator --list` prints what is configured and whether each command was found.

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

One `call_<name>` per configured agent, plus `list_agents`.

Arguments: `prompt` (required), `cwd`, `timeout_seconds` (default 300, max 600).
Result JSON: `agent`, `exit_code`, `duration_ms`, `output`, `stderr`, `timed_out`.

## Environment

| variable                 | meaning                                            |
|--------------------------|----------------------------------------------------|
| `DELEGATOR_CONFIG`       | path of the agents config file                     |
| `DELEGATOR_AGENT_<NAME>` | one agent as `<command> <args...>`                 |
| `DELEGATOR_DEFAULT_CWD`  | working directory when the call does not set `cwd` |

## License

MIT
