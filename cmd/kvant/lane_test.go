package main

import "testing"

// A pool could only ever hold boxes that agreed on where chatgpt-tool lived,
// because --tool is one flag for the whole run. Ours do not agree, so the odd
// one out was left out of every pool and sat idle while the other two were
// capped.
func TestAHostCanCarryItsOwnToolPath(t *testing.T) {
	for _, c := range []struct {
		spec, fallback string
		name, tool     string
	}{
		{"server2", FleetTool, "server2", FleetTool},
		{"server1=/home/tam/chatgpt-tool/.venv/bin/chatgpt-tool", FleetTool,
			"server1", "/home/tam/chatgpt-tool/.venv/bin/chatgpt-tool"},
		// A name with nothing after the sign is a typo rather than an instruction
		// to run the empty string, and the fallback is the safer reading.
		{"server3=", FleetTool, "server3", FleetTool},
	} {
		name, tool := splitHost(c.spec, c.fallback)
		if name != c.name || tool != c.tool {
			t.Errorf("splitHost(%q) = %q, %q, want %q, %q", c.spec, name, tool, c.name, c.tool)
		}
	}
}
