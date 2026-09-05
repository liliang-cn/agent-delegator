package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Agent describes how to run one agent CLI in one-shot mode.
//
// Args is an argv template. "{prompt}" is replaced by the task; when the
// placeholder is absent the prompt is appended as the last argument.
type Agent struct {
	Name        string            `json:"name"`
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Description string            `json:"description,omitempty"`

	// resolved is the executable path, or "" when the command was not found.
	resolved string
}

type configFile struct {
	Agents []Agent `json:"agents"`
}

func configPath() string {
	if v := strings.TrimSpace(os.Getenv("DELEGATOR_CONFIG")); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "agent-delegator", "agents.json")
}

// loadAgents reads the config file, then DELEGATOR_AGENT_<NAME> variables
// ("<command> <args...>", whitespace separated, {prompt} as one token), which
// add to or override file entries of the same name.
func loadAgents() ([]Agent, error) {
	byName := map[string]Agent{}

	p := configPath()
	if p != "" {
		b, err := os.ReadFile(p)
		switch {
		case err == nil:
			var cf configFile
			if err := json.Unmarshal(b, &cf); err != nil {
				return nil, fmt.Errorf("parse %s: %w", p, err)
			}
			for _, a := range cf.Agents {
				if a.Name == "" {
					return nil, fmt.Errorf("%s: agent without name", p)
				}
				if a.Command == "" {
					a.Command = a.Name
				}
				byName[a.Name] = a
			}
		case !os.IsNotExist(err):
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
	}

	const prefix = "DELEGATOR_AGENT_"
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || !strings.HasPrefix(k, prefix) {
			continue
		}
		name := strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(k, prefix), "_", "-"))
		fields := strings.Fields(v)
		if name == "" || len(fields) == 0 {
			continue
		}
		byName[name] = Agent{Name: name, Command: fields[0], Args: fields[1:]}
	}

	out := make([]Agent, 0, len(byName))
	for _, a := range byName {
		a.resolved = resolve(a.Command)
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func resolve(command string) string {
	if strings.Contains(command, "/") {
		if st, err := os.Stat(command); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return command
		}
		return ""
	}
	if p, err := exec.LookPath(command); err == nil {
		return p
	}
	return ""
}

var nonIdent = regexp.MustCompile(`[^a-z0-9]+`)

// toolName turns "cursor-agent" into "call_cursor_agent".
func toolName(name string) string {
	return "call_" + strings.Trim(nonIdent.ReplaceAllString(strings.ToLower(name), "_"), "_")
}

func (a Agent) argv(prompt string) []string {
	if len(a.Args) == 0 {
		return []string{prompt}
	}
	out := make([]string, 0, len(a.Args)+1)
	placed := false
	for _, s := range a.Args {
		if strings.Contains(s, "{prompt}") {
			s = strings.ReplaceAll(s, "{prompt}", prompt)
			placed = true
		}
		out = append(out, s)
	}
	if !placed {
		out = append(out, prompt)
	}
	return out
}
