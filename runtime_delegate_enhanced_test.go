package tool

import (
	"encoding/json"
	"testing"
)

func TestDelegateToSkillTool(t *testing.T) {
	dm := NewDelegateManager(DefaultDelegateConfig())
	r := NewRegistry()
	r.Register(DelegateToSkillTool(dm))

	result, err := r.Call("delegate_to_skill", map[string]any{
		"skill_name":  "web-search",
		"description": "Search for Go tutorials",
		"priority":    "high",
	})
	if err != nil {
		t.Fatalf("delegate_to_skill: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("parse result: %v", err)
	}

	if resp["skill_name"] != "web-search" {
		t.Errorf("expected skill_name=web-search, got %v", resp["skill_name"])
	}
	if resp["priority"] != "high" {
		t.Errorf("expected priority=high, got %v", resp["priority"])
	}
}

func TestDelegateToMCPTool(t *testing.T) {
	dm := NewDelegateManager(DefaultDelegateConfig())
	r := NewRegistry()
	r.Register(DelegateToMCPTool(dm))

	result, err := r.Call("delegate_to_mcp", map[string]any{
		"server_name": "test-server",
		"tool_name":   "search",
		"priority":    "critical",
	})
	if err != nil {
		t.Fatalf("delegate_to_mcp: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("parse result: %v", err)
	}

	if resp["server_name"] != "test-server" {
		t.Errorf("expected server_name=test-server, got %v", resp["server_name"])
	}
	if resp["priority"] != "critical" {
		t.Errorf("expected priority=critical, got %v", resp["priority"])
	}
}

func TestDelegateToSkillToolMissingArgs(t *testing.T) {
	dm := NewDelegateManager(DefaultDelegateConfig())
	r := NewRegistry()
	r.Register(DelegateToSkillTool(dm))

	_, err := r.Call("delegate_to_skill", map[string]any{})
	if err == nil {
		t.Error("expected error for missing skill_name")
	}
}

func TestDelegateToMCPToolMissingArgs(t *testing.T) {
	dm := NewDelegateManager(DefaultDelegateConfig())
	r := NewRegistry()
	r.Register(DelegateToMCPTool(dm))

	_, err := r.Call("delegate_to_mcp", map[string]any{})
	if err == nil {
		t.Error("expected error for missing server_name")
	}
}

func TestDelegateToSkillToolCategories(t *testing.T) {
	dm := NewDelegateManager(DefaultDelegateConfig())
	tool := DelegateToSkillTool(dm)

	if tool.Category != CatDelegate {
		t.Errorf("expected CatDelegate, got %s", tool.Category)
	}
	if tool.Permission != PermApprove {
		t.Errorf("expected PermApprove, got %s", tool.Permission)
	}
}

func TestDelegateToMCPToolCategories(t *testing.T) {
	dm := NewDelegateManager(DefaultDelegateConfig())
	tool := DelegateToMCPTool(dm)

	if tool.Category != CatDelegate {
		t.Errorf("expected CatDelegate, got %s", tool.Category)
	}
	if tool.Permission != PermApprove {
		t.Errorf("expected PermApprove, got %s", tool.Permission)
	}
}
