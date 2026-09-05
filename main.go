// agent-delegator exposes the pi coding agent and Hermes Agent that live on the
// openclaw cluster's DRBD volume as MCP tools, so the openclaw main agent can
// hand work to them when the user writes "@pi ..." or "@hermes ...".
//
// It replaces a hand-rolled Python stdio server. Two lessons from that one are
// baked in here:
//   - The child's stdin must be closed (/dev/null). `pi -p` blocks until it sees
//     EOF on a non-TTY stdin, and inheriting the MCP JSON-RPC pipe means EOF
//     never comes, so every call timed out on the openclaw side.
//   - A timeout must kill the whole process group, not just the wrapper shell,
//     otherwise a stuck node/python child keeps running after we give up.
//
// Model-facing text (tool names, descriptions, result fields) is English only.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultPiBin     = "/var/lib/openclaw/pi/pi"
	defaultHermesBin = "/var/lib/openclaw/hermes/hermes"
	defaultCwd       = "/var/lib/openclaw/workspace"
	defaultTimeout   = 300 * time.Second
	maxTimeout       = 600 * time.Second
	maxOutputBytes   = 60 * 1024
)

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

type delegateArgs struct {
	Prompt         string `json:"prompt" jsonschema:"the complete task for the sub-agent, self-contained: it has no access to this conversation"`
	Cwd            string `json:"cwd,omitempty" jsonschema:"working directory for the sub-agent, defaults to the openclaw workspace"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"max seconds to wait, 1-600, default 300"`
}

type delegateResult struct {
	Agent      string `json:"agent"`
	ExitCode   int    `json:"exit_code"`
	TimedOut   bool   `json:"timed_out,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	Output     string `json:"output"`
	Stderr     string `json:"stderr,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
	Note       string `json:"note,omitempty"`
}

// agentSpec describes how to invoke one sub-agent in one-shot mode.
type agentSpec struct {
	name string
	bin  string
	// args builds the argv (after the binary) for a given prompt.
	args func(prompt string) []string
}

func truncate(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	return s[:n] + "\n…[truncated]", true
}

func runAgent(ctx context.Context, spec agentSpec, a delegateArgs) (*mcp.CallToolResult, any, error) {
	prompt := strings.TrimSpace(a.Prompt)
	if prompt == "" {
		return nil, nil, errors.New("prompt is required")
	}
	cwd := a.Cwd
	if cwd == "" {
		cwd = envOr("DELEGATOR_DEFAULT_CWD", defaultCwd)
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		return nil, nil, fmt.Errorf("cwd %q is not a directory", cwd)
	}
	timeout := defaultTimeout
	if a.TimeoutSeconds > 0 {
		timeout = time.Duration(a.TimeoutSeconds) * time.Second
		if timeout > maxTimeout {
			timeout = maxTimeout
		}
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, spec.bin, spec.args(prompt)...)
	cmd.Dir = cwd
	cmd.Stdin = nil // /dev/null: never let the child wait on our JSON-RPC pipe
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Own process group so a timeout takes the whole tree down with it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 5 * time.Second

	start := time.Now()
	err := cmd.Run()
	dur := time.Since(start)

	res := delegateResult{Agent: spec.name, DurationMs: dur.Milliseconds()}
	res.Output, res.Truncated = truncate(strings.TrimSpace(stdout.String()), maxOutputBytes)
	errText, _ := truncate(strings.TrimSpace(stderr.String()), 4*1024)

	isErr := false
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		res.TimedOut = true
		res.ExitCode = -1
		res.Note = fmt.Sprintf("%s did not finish within %s and was killed", spec.name, timeout)
		res.Stderr = errText
		isErr = true
	case err != nil:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
		} else {
			res.ExitCode = -1
			res.Note = "failed to start: " + err.Error()
		}
		res.Stderr = errText
		isErr = true
	default:
		res.ExitCode = 0
		if res.Output == "" {
			// Some agents print their answer on stderr when stdout is not a TTY.
			res.Output = errText
		}
	}
	log.Printf("%s exit=%d timed_out=%v dur=%s out=%dB err=%dB", spec.name, res.ExitCode, res.TimedOut, dur.Round(time.Millisecond), stdout.Len(), stderr.Len())

	b, _ := json.MarshalIndent(res, "", "  ")
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
		IsError: isErr,
	}, nil, nil
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime)
	log.SetPrefix("agent-delegator: ")

	pi := agentSpec{
		name: "pi",
		bin:  envOr("DELEGATOR_PI_BIN", defaultPiBin),
		args: func(p string) []string { return []string{"-p", p} },
	}
	hermes := agentSpec{
		name: "hermes",
		bin:  envOr("DELEGATOR_HERMES_BIN", defaultHermesBin),
		args: func(p string) []string { return []string{"-z", p} },
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "agent-delegator",
		Version: "2.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name: "call_pi",
		Description: "Delegate a task to the pi coding agent, a separate autonomous agent with its own shell, file and web_search tools running on this host. " +
			"You MUST call this tool whenever the user's message addresses @pi, and pass the user's request as the prompt. " +
			"Also suitable for coding, refactoring, running commands and inspecting files. " +
			"The agent has no memory of this conversation, so the prompt must be self-contained. " +
			"Report its output to the user as pi's answer; never invent a result if the call fails.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a delegateArgs) (*mcp.CallToolResult, any, error) {
		return runAgent(ctx, pi, a)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "call_hermes",
		Description: "Delegate a task to Hermes Agent, a separate autonomous general-purpose agent with shell, file, browser and web_search tools running on this host. " +
			"You MUST call this tool whenever the user's message addresses @hermes, and pass the user's request as the prompt. " +
			"Also suitable for multi-step research, planning and long-running autonomous work. " +
			"The agent has no memory of this conversation, so the prompt must be self-contained. " +
			"Report its output to the user as Hermes' answer; never invent a result if the call fails.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a delegateArgs) (*mcp.CallToolResult, any, error) {
		return runAgent(ctx, hermes, a)
	})

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
