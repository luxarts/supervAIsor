package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// Manager tracks running Claude Code subprocesses.
type Manager struct {
	mu       sync.Mutex
	commands map[string]*exec.Cmd
}

func NewManager() *Manager {
	return &Manager{commands: make(map[string]*exec.Cmd)}
}

// SpawnConfig holds everything needed to start a new agent.
type SpawnConfig struct {
	AgentID  string
	WorkDir  string
	Prompt   string
	HookBase string // e.g. "http://localhost:8080"
}

// Spawn writes the hook config for this agent, then starts the claude subprocess.
// Returns the started *exec.Cmd so the caller can read the PID.
func (m *Manager) Spawn(cfg SpawnConfig) (*exec.Cmd, error) {
	if err := m.writeHookConfig(cfg); err != nil {
		return nil, fmt.Errorf("write hook config: %w", err)
	}

	cmd := exec.Command("claude", "--dangerously-skip-permissions", "-p", cfg.Prompt)
	cmd.Dir = cfg.WorkDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start claude: %w", err)
	}

	m.mu.Lock()
	m.commands[cfg.AgentID] = cmd
	m.mu.Unlock()

	// Reap the process in the background so it doesn't become a zombie.
	go func() {
		cmd.Wait() //nolint:errcheck
		m.mu.Lock()
		delete(m.commands, cfg.AgentID)
		m.mu.Unlock()
	}()

	return cmd, nil
}

// Kill sends SIGKILL to the agent subprocess.
func (m *Manager) Kill(agentID string) error {
	m.mu.Lock()
	cmd, ok := m.commands[agentID]
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("agent %s not found or already stopped", agentID)
	}

	if cmd.Process != nil {
		if err := cmd.Process.Kill(); err != nil {
			return fmt.Errorf("kill process: %w", err)
		}
	}

	return nil
}

// ---- Hook config generation -----------------------------------------------

type hookConfig struct {
	Hooks map[string][]hookMatcher `json:"hooks"`
}

type hookMatcher struct {
	Matcher string    `json:"matcher"`
	Hooks   []hookDef `json:"hooks"`
}

type hookDef struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

func (m *Manager) writeHookConfig(cfg SpawnConfig) error {
	claudeDir := filepath.Join(cfg.WorkDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return err
	}

	curlCmd := func(event string) string {
		url := fmt.Sprintf("%s/hooks?agent_id=%s&event=%s", cfg.HookBase, cfg.AgentID, event)
		return fmt.Sprintf(`curl -s -X POST "%s" -H "Content-Type: application/json" --data-binary @-`, url)
	}

	matcher := func(event string) hookMatcher {
		return hookMatcher{
			Matcher: ".*",
			Hooks:   []hookDef{{Type: "command", Command: curlCmd(event)}},
		}
	}

	config := hookConfig{
		Hooks: map[string][]hookMatcher{
			"PreToolUse":   {matcher("PreToolUse")},
			"PostToolUse":  {matcher("PostToolUse")},
			"Notification": {matcher("Notification")},
			"Stop":         {matcher("Stop")},
		},
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(claudeDir, "settings.json"), data, 0o644)
}
