package fleet

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// State is what fleet up leaves behind so that fleet status, fleet down and
// every other command in another process can find the tunnels.
//
// It is discovered fact and running state, never configuration. The
// configuration is the route file. Nothing here is worth keeping if it is
// stale, which is why Load says how old it is rather than pretending.
type State struct {
	Written time.Time        `json:"written"`
	Hosts   map[string]Facts `json:"hosts,omitempty"`
	Tunnels []Tunnel         `json:"tunnels,omitempty"`
}

// Tunnel is one ssh port forward.
type Tunnel struct {
	Host       string    `json:"host"`
	Route      string    `json:"route"`
	LocalPort  int       `json:"local_port"`
	RemotePort int       `json:"remote_port"`
	PID        int       `json:"pid"`
	Started    time.Time `json:"started"`
}

// StatePath is where the running state lives.
func StatePath() string {
	if value := strings.TrimSpace(os.Getenv("BOURBAKI_FLEET_STATE")); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "fleet.json"
	}
	return filepath.Join(home, ".config", "kvant", "fleet.json")
}

// LoadState reads the state file. A missing file is not an error: it means no
// fleet has been brought up in this configuration yet.
func LoadState(path string) (State, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return State{Hosts: map[string]Facts{}}, nil
	}
	if err != nil {
		return State{}, err
	}
	var value State
	if err := json.Unmarshal(raw, &value); err != nil {
		return State{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if value.Hosts == nil {
		value.Hosts = map[string]Facts{}
	}
	return value, nil
}

// Save writes the state atomically, because fleet status in one terminal
// reading a half written file while fleet up in another is writing it is a
// confusing failure to debug and a trivial one to prevent.
func (s State) Save(path string) error {
	if directory := filepath.Dir(path); directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

// Tool is the chatgpt-tool path discovered on a host, which differs per host
// and so cannot be assumed.
func (s State) Tool(host string) (string, bool) {
	facts, ok := s.Hosts[host]
	if !ok || facts.Tool == "" {
		return "", false
	}
	return facts.Tool, true
}

// Find returns the tunnel for a route.
func (s State) Find(route string) (Tunnel, bool) {
	for _, value := range s.Tunnels {
		if value.Route == route {
			return value, true
		}
	}
	return Tunnel{}, false
}
