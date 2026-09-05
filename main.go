// agent-delegator exposes agent CLIs on this host as MCP tools, so a chat
// agent can hand a task to "@pi", "@codex", "@hermes" and so on. Every agent
// declared in the config becomes one call_<name> tool; list_agents reports them.
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
	version        = "3.0.0"
	defaultTimeout = 300 * time.Second
	maxTimeout     = 600 * time.Second
	maxOutputBytes = 60 * 1024
	clusterCwd     = "/var/lib/openclaw/workspace"
)

type delegateArgs struct {
	Prompt         string `json:"prompt" jsonschema:"the complete task for the sub-agent, self-contained: it has no access to this conversation"`
	Cwd            string `json:"cwd,omitempty" jsonschema:"working directory for the sub-agent, defaults to DELEGATOR_DEFAULT_CWD"`
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

type listArgs struct{}

type agentInfo struct {
	Name        string `json:"name"`
	Tool        string `json:"tool"`
	Description string `json:"description,omitempty"`
	Command     string `json:"command"`
	Args        string `json:"args"`
	Found       bool   `json:"found"`
}

func defaultCwd() string {
	if v := strings.TrimSpace(os.Getenv("DELEGATOR_DEFAULT_CWD")); v != "" {
		return v
	}
	if st, err := os.Stat(clusterCwd); err == nil && st.IsDir() {
		return clusterCwd
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "/"
}

func truncate(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	return s[:n] + "\n…[truncated]", true
}

func runAgent(ctx context.Context, a Agent, in delegateArgs) (*mcp.CallToolResult, any, error) {
	prompt := strings.TrimSpace(in.Prompt)
	if prompt == "" {
		return nil, nil, errors.New("prompt is required")
	}
	cwd := in.Cwd
	if cwd == "" {
		cwd = defaultCwd()
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		return nil, nil, fmt.Errorf("cwd %q is not a directory", cwd)
	}
	timeout := defaultTimeout
	if in.TimeoutSeconds > 0 {
		timeout = time.Duration(in.TimeoutSeconds) * time.Second
		if timeout > maxTimeout {
			timeout = maxTimeout
		}
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if a.resolved == "" {
		return nil, nil, fmt.Errorf("agent %q: command %q not found", a.Name, a.Command)
	}
	cmd := exec.CommandContext(ctx, a.resolved, a.argv(prompt)...)
	cmd.Dir = cwd
	cmd.Stdin = nil // /dev/null: never let the child wait on our JSON-RPC pipe
	cmd.Env = os.Environ()
	for k, v := range a.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Own process group so a timeout takes the whole tree down with it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second

	start := time.Now()
	err := cmd.Run()
	dur := time.Since(start)

	res := delegateResult{Agent: a.Name, DurationMs: dur.Milliseconds()}
	res.Output, res.Truncated = truncate(strings.TrimSpace(stdout.String()), maxOutputBytes)
	errText, _ := truncate(strings.TrimSpace(stderr.String()), 4*1024)

	isErr := false
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		res.TimedOut, res.ExitCode, isErr = true, -1, true
		res.Note = fmt.Sprintf("%s did not finish within %s and was killed", a.Name, timeout)
		res.Stderr = errText
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
		if res.Output == "" {
			// Some agents print their answer on stderr when stdout is not a TTY.
			res.Output = errText
		}
	}
	log.Printf("%s exit=%d timed_out=%v dur=%s out=%dB err=%dB", a.Name, res.ExitCode, res.TimedOut, dur.Round(time.Millisecond), stdout.Len(), stderr.Len())

	b, _ := json.MarshalIndent(res, "", "  ")
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
		IsError: isErr,
	}, nil, nil
}

func describe(a Agent) string {
	label := a.Name
	if a.Description != "" {
		label = a.Description + " (" + a.Name + ")"
	}
	return fmt.Sprintf("Delegate a task to %s, a separate autonomous agent CLI running on this host with its own shell, file and code tools. "+
		"You MUST call this tool whenever the user's message addresses @%s, passing the user's request as the prompt. "+
		"The agent has no memory of this conversation, so the prompt must be self-contained. "+
		"Report its output to the user as %s's answer; never invent a result if the call fails.",
		label, a.Name, a.Name)
}

func infos(agents []Agent) []agentInfo {
	out := make([]agentInfo, 0, len(agents))
	for _, a := range agents {
		out = append(out, agentInfo{
			Name: a.Name, Tool: toolName(a.Name), Description: a.Description,
			Command: a.Command, Args: strings.Join(a.Args, " "), Found: a.resolved != "",
		})
	}
	return out
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime)
	log.SetPrefix("agent-delegator: ")

	agents, err := loadAgents()
	if err != nil {
		log.Fatal(err)
	}

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--list", "list":
			if len(agents) == 0 {
				fmt.Printf("no agents configured (%s)\n", configPath())
			}
			for _, a := range agents {
				where := a.resolved
				if where == "" {
					where = a.Command + " (not found)"
				}
				fmt.Printf("%-14s %-20s %s %s\n", a.Name, toolName(a.Name), where, strings.Join(a.Args, " "))
			}
			return
		case "--version", "version":
			fmt.Println(version)
			return
		default:
			fmt.Fprintf(os.Stderr, "usage: agent-delegator [--list | --version]\n")
			os.Exit(2)
		}
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "agent-delegator", Version: version}, nil)

	for _, a := range agents {
		a := a
		mcp.AddTool(server, &mcp.Tool{Name: toolName(a.Name), Description: describe(a)},
			func(ctx context.Context, _ *mcp.CallToolRequest, in delegateArgs) (*mcp.CallToolResult, any, error) {
				return runAgent(ctx, a, in)
			})
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_agents",
		Description: "List the agent CLIs configured on this host and the call_<name> tool for each.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ listArgs) (*mcp.CallToolResult, any, error) {
		b, _ := json.MarshalIndent(infos(agents), "", "  ")
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil, nil
	})

	log.Printf("%d agent(s): %s", len(agents), strings.Join(names(agents), ", "))
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

func names(agents []Agent) []string {
	out := make([]string, 0, len(agents))
	for _, a := range agents {
		out = append(out, a.Name)
	}
	return out
}
