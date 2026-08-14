package route

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The file below is the one spec 04 §3 describes, byte for byte in shape. It is
// here so a change to the struct tags fails a test rather than a run.
const routeFile = `{
  "routes": [
    {"name": "server3", "wire": "chat", "base_url": "http://127.0.0.1:18773/v1",
     "model": "gpt-5", "api_key_env": "BOURBAKI_PROXY_KEY", "rank": 10,
     "concurrency": 4, "timeout": "20m", "host": "server3", "local_port": 18773, "remote_port": 8077},
    {"name": "server1", "wire": "chat", "base_url": "http://127.0.0.1:18771/v1",
     "model": "gpt-5", "api_key_env": "BOURBAKI_PROXY_KEY", "rank": 20,
     "concurrency": 1, "timeout": "20m", "host": "server1", "local_port": 18771, "remote_port": 8077},
    {"name": "server2", "wire": "chat", "base_url": "http://127.0.0.1:18772/v1",
     "model": "gpt-5", "api_key_env": "BOURBAKI_PROXY_KEY", "rank": 30,
     "concurrency": 2, "timeout": "20m", "disabled": true,
     "note": "serve not running as of 2026-08-10"}
  ]
}
`

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "routes.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	registry, err := Load(write(t, routeFile))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := registry.Names(); strings.Join(got, ",") != "server3,server1,server2" {
		t.Errorf("Names = %v, want rank order", got)
	}
	enabled := registry.Enabled()
	if len(enabled) != 2 {
		t.Fatalf("Enabled = %d routes, want 2 with server2 off", len(enabled))
	}
	if enabled[0].Timeout.Duration() != 20*time.Minute {
		t.Errorf("timeout = %s, want 20m", enabled[0].Timeout.Duration())
	}
	if enabled[0].Lanes() != 4 || enabled[1].Lanes() != 1 {
		t.Errorf("lanes = %d, %d; want 4, 1", enabled[0].Lanes(), enabled[1].Lanes())
	}
}

// A route file must never carry the key itself, only the name of the variable
// holding it. Round tripping one through Write is where a literal key would
// leak, so the field has to stay unmarshalled.
func TestKeyNeverRoundTrips(t *testing.T) {
	registry := Registry{Routes: []Route{{
		Name: "server3", Wire: WireChat, BaseURL: "http://127.0.0.1:18773/v1",
		Model: "gpt-5", APIKey: "sk-secret", APIKeyEnv: KeyEnv, Rank: 10,
	}}}
	raw, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-secret") {
		t.Fatalf("the key was written to the route file:\n%s", raw)
	}
	if !strings.Contains(string(raw), KeyEnv) {
		t.Errorf("the variable name was dropped:\n%s", raw)
	}
}

func TestKeyPrefersLiteral(t *testing.T) {
	t.Setenv(KeyEnv, "sk-from-env")
	value := Route{APIKeyEnv: KeyEnv}
	if got := value.Key(); got != "sk-from-env" {
		t.Errorf("Key = %q, want the environment", got)
	}
	value.APIKey = "sk-from-flag"
	if got := value.Key(); got != "sk-from-flag" {
		t.Errorf("Key = %q, want the flag to win", got)
	}
}

// Two tunnels on one local port is not a typo anyone finds later: the second
// ssh fails, the first keeps serving, and every call meant for the second host
// lands on the first.
func TestDuplicateLocalPortRejected(t *testing.T) {
	body := `{"routes":[
	  {"name":"a","wire":"chat","base_url":"http://127.0.0.1:18773/v1","model":"gpt-5","local_port":18773},
	  {"name":"b","wire":"chat","base_url":"http://127.0.0.1:18773/v1","model":"gpt-5","local_port":18773}]}`
	_, err := Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "local port 18773") {
		t.Fatalf("err = %v, want a complaint about the shared port", err)
	}
}

func TestValidate(t *testing.T) {
	for _, c := range []struct{ body, want string }{
		{`{"routes":[]}`, "lists no routes"},
		{`{"routes":[{"wire":"chat","base_url":"http://x/v1","model":"m"}]}`, "no name"},
		{`{"routes":[{"name":"a","wire":"chat","base_url":"http://x/v1"}]}`, "no model"},
		{`{"routes":[{"name":"a","wire":"chat","model":"m"}]}`, "no base_url"},
		{`{"routes":[{"name":"a","wire":"grpc","base_url":"http://x/v1","model":"m"}]}`, "unknown wire"},
		{`{"routes":[{"name":"a","wire":"chat","base_url":"http://x/v1","model":"m"},
		             {"name":"a","wire":"chat","base_url":"http://y/v1","model":"m"}]}`, "listed twice"},
	} {
		_, err := Load(write(t, c.body))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Load(%s) = %v, want %q", c.body, err, c.want)
		}
	}
}

// A base URL may stop at the root or end at /v1, because both are things people
// paste, and a doubled /v1/v1 is a 404 that reads as a dead host.
func TestEndpoint(t *testing.T) {
	for _, c := range []struct{ base, want string }{
		{"http://127.0.0.1:18773/v1", "http://127.0.0.1:18773/v1/health"},
		{"http://127.0.0.1:18773", "http://127.0.0.1:18773/v1/health"},
		{"http://127.0.0.1:18773/v1/", "http://127.0.0.1:18773/v1/health"},
	} {
		if got := (Route{BaseURL: c.base}).Endpoint("/health"); got != c.want {
			t.Errorf("Endpoint(%q) = %q, want %q", c.base, got, c.want)
		}
	}
}

func TestDurationJSON(t *testing.T) {
	var value struct {
		D Duration `json:"d"`
	}
	if err := json.Unmarshal([]byte(`{"d":"20m"}`), &value); err != nil || value.D.Duration() != 20*time.Minute {
		t.Errorf("string form: %s, %v", value.D.Duration(), err)
	}
	// A bare number is what someone hand editing the file is most likely to
	// have meant as seconds.
	if err := json.Unmarshal([]byte(`{"d":90}`), &value); err != nil || value.D.Duration() != 90*time.Second {
		t.Errorf("number form: %s, %v", value.D.Duration(), err)
	}
	raw, err := json.Marshal(value)
	if err != nil || !strings.Contains(string(raw), `"1m30s"`) {
		t.Errorf("Marshal = %s, %v", raw, err)
	}
}

func TestDefaultIsTheMeasuredFleet(t *testing.T) {
	registry := Default()
	if err := registry.Validate(); err != nil {
		t.Fatalf("the built-in fleet does not validate: %v", err)
	}
	if got := registry.Names(); strings.Join(got, ",") != "server3,server2,server1" {
		t.Errorf("Names = %v; the order is verified profiles then free memory", got)
	}
	// Every route is enabled and every one carries the numbers it was ranked
	// on, so a rank that stops matching the fleet is visible in the diff rather
	// than only in a slow run.
	for _, want := range []struct {
		name        string
		concurrency int
	}{{"server3", 4}, {"server2", 3}, {"server1", 1}} {
		value, ok := registry.Find(want.name)
		if !ok {
			t.Fatalf("%s is missing from the built-in fleet", want.name)
		}
		if value.Disabled {
			t.Errorf("%s is disabled", want.name)
		}
		if value.Concurrency != want.concurrency {
			t.Errorf("%s concurrency = %d, want the %d it declares over /v1/health",
				want.name, value.Concurrency, want.concurrency)
		}
		if value.Note == "" {
			t.Errorf("%s has no note saying what it was ranked on", want.name)
		}
	}
	if got := NewPool(registry).Lanes(); got != 8 {
		t.Errorf("the fleet carries %d calls at once, want 8", got)
	}
}

func TestSelectOverridesDisabled(t *testing.T) {
	registry, err := Load(write(t, routeFile))
	if err != nil {
		t.Fatal(err)
	}
	// Naming a route on the command line is the override, so a disabled one
	// comes back enabled and in the order asked for.
	got, err := registry.Select([]string{"server2", "server1"})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if strings.Join(got.Names(), ",") != "server2,server1" {
		t.Errorf("Names = %v, want the caller's order", got.Names())
	}
	if got.Routes[0].Disabled {
		t.Error("server2 is still disabled after being named")
	}
	if _, err := registry.Select([]string{"server9"}); err == nil {
		t.Error("an unknown route was accepted")
	}
}
