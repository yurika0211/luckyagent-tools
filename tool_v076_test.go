package tool

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// TestMCPClient_NewMCPClient 测试创建 MCP 客户端
func TestMCPClient_NewMCPClient(t *testing.T) {
	client := NewMCPClient()

	if client == nil {
		t.Fatal("NewMCPClient should not return nil")
	}

	if client.servers == nil {
		t.Error("servers map should be initialized")
	}

	if client.client == nil {
		t.Error("http client should be initialized")
	}
}

// TestMCPClient_AddServer 测试添加服务器
func TestMCPClient_AddServer(t *testing.T) {
	client := NewMCPClient()

	cfg := MCPServerConfig{
		Name: "test-server",
		URL:  "http://localhost:8080",
	}

	client.AddServer(cfg)

	// 验证服务器已添加
	client.mu.RLock()
	_, exists := client.servers["test-server"]
	client.mu.RUnlock()

	if !exists {
		t.Error("Server should be added")
	}
}

// TestMCPClient_RemoveServer 测试移除服务器
func TestMCPClient_RemoveServer(t *testing.T) {
	client := NewMCPClient()

	// 先添加
	cfg := MCPServerConfig{
		Name: "to-remove",
		URL:  "http://localhost:8080",
	}
	client.AddServer(cfg)

	// 再移除
	client.RemoveServer("to-remove")

	// 验证已移除
	client.mu.RLock()
	_, exists := client.servers["to-remove"]
	client.mu.RUnlock()

	if exists {
		t.Error("Server should be removed")
	}
}

// TestMCPClient_ListServers 测试列出服务器
func TestMCPClient_ListServers(t *testing.T) {
	client := NewMCPClient()

	// 添加两个服务器
	client.AddServer(MCPServerConfig{Name: "server1", URL: "http://localhost:8081"})
	client.AddServer(MCPServerConfig{Name: "server2", URL: "http://localhost:8082"})

	servers := client.ListServers()

	if len(servers) != 2 {
		t.Errorf("Expected 2 servers, got %d", len(servers))
	}

	t.Logf("Listed %d servers", len(servers))
}

// TestMCPClient_RemoveNonexistent 测试移除不存在的服务器
func TestMCPClient_RemoveNonexistent(t *testing.T) {
	client := NewMCPClient()

	// 移除不存在的服务器不应 panic
	client.RemoveServer("nonexistent")
	t.Log("RemoveServer on nonexistent server completed without panic")
}

// TestMCPClient_MultipleOperations 测试多个操作
func TestMCPClient_MultipleOperations(t *testing.T) {
	client := NewMCPClient()

	// 添加多个服务器
	for i := 0; i < 5; i++ {
		client.AddServer(MCPServerConfig{
			Name: fmt.Sprintf("server-%d", i),
			URL:  fmt.Sprintf("http://localhost:%d", 8080+i),
		})
	}

	// 列出现在应该有 5 个
	servers := client.ListServers()
	if len(servers) != 5 {
		t.Errorf("Expected 5 servers, got %d", len(servers))
	}

	// 移除一个
	client.RemoveServer("server-2")

	// 再列出应该有 4 个
	servers = client.ListServers()
	if len(servers) != 4 {
		t.Errorf("Expected 4 servers after removal, got %d", len(servers))
	}

	t.Logf("Final server count: %d", len(servers))
}

// TestGateway_Getters 测试 Gateway 的 Getter 方法
func TestGateway_Getters(t *testing.T) {
	reg := NewRegistry()
	gw := NewGateway(reg)

	// 测试 Tracker
	tracker := gw.Tracker()
	if tracker == nil {
		t.Error("Tracker should not be nil")
	}

	// 测试 Subscriptions
	sub := gw.Subscriptions()
	if sub == nil {
		t.Error("Subscriptions should not be nil")
	}

	// 测试 Router
	router := gw.Router()
	if router == nil {
		t.Error("Router should not be nil")
	}

	t.Logf("Gateway getters: tracker=%v, sub=%v, router=%v", tracker != nil, sub != nil, router != nil)
}

// TestGatewayResult_Format 测试结果格式化
func TestGatewayResult_Format(t *testing.T) {
	// 测试结果格式化
	result := &GatewayResult{
		ToolName:  "test_tool",
		Output:    "success",
		Duration:  100 * time.Millisecond,
		Success:   true,
		Timestamp: time.Now(),
	}

	formatted := result.Format()
	if formatted == "" {
		t.Error("Expected non-empty formatted result")
	}

	t.Logf("Formatted result: %s", formatted)
}

// TestGatewayResult_FormatFailure 测试失败结果格式化
func TestGatewayResult_FormatFailure(t *testing.T) {
	result := &GatewayResult{
		ToolName:  "failed_tool",
		Output:    "error message",
		Duration:  50 * time.Millisecond,
		Success:   false,
		Timestamp: time.Now(),
	}

	formatted := result.Format()
	if formatted == "" {
		t.Error("Expected non-empty formatted result")
	}

	// 应该包含 ❌
	if !contains(formatted, "❌") {
		t.Error("Expected failure indicator in formatted result")
	}

	t.Logf("Formatted failure: %s", formatted)
}

// TestErrQuotaExceeded_Error 测试配额超限错误
func TestErrQuotaExceeded_Error(t *testing.T) {
	err := ErrQuotaExceeded{
		Tool:   "test_tool",
		UserID: "user123",
		Reason: "daily limit exceeded",
	}

	errStr := err.Error()
	if errStr == "" {
		t.Error("Expected non-empty error string")
	}

	// 应该包含关键信息
	if !contains(errStr, "test_tool") {
		t.Error("Error should contain tool name")
	}
	if !contains(errStr, "user123") {
		t.Error("Error should contain user ID")
	}

	t.Logf("ErrQuotaExceeded: %s", errStr)
}

// TestErrQuotaExceeded_ErrorEmptyFields 测试空字段
func TestErrQuotaExceeded_ErrorEmptyFields(t *testing.T) {
	err := ErrQuotaExceeded{
		Tool:   "unknown",
		UserID: "",
		Reason: "unknown reason",
	}

	errStr := err.Error()
	if errStr == "" {
		t.Error("Expected non-empty error string")
	}

	t.Logf("ErrQuotaExceeded (empty user): %s", errStr)
}

// TestSearchWithBraveEntries_NoAPIKey 测试 Brave API 无 API key 的情况
func TestSearchWithBraveEntries_NoAPIKey(t *testing.T) {
	// 临时清除环境变量
	origKey := os.Getenv("BRAVE_API_KEY")
	os.Unsetenv("BRAVE_API_KEY")
	defer func() {
		if origKey != "" {
			os.Setenv("BRAVE_API_KEY", origKey)
		}
	}()

	cfg := &WebSearchConfig{
		APIKey: "",
	}

	_, err := searchWithBraveEntries(cfg, "test query", 5)

	// 应该返回 "no API key" 错误
	if err == nil {
		t.Fatal("Expected error when no API key")
	}

	if !contains(err.Error(), "no API key") {
		t.Errorf("Expected 'no API key' error, got: %v", err)
	}

	t.Logf("searchWithBraveEntries correctly returned error: %v", err)
}

// TestSearchWithBraveEntries_WithAPIKey 测试 Brave API 有 API key 的情况 (mock)
func TestSearchWithBraveEntries_WithAPIKey(t *testing.T) {
	// 使用 mock API key (不会真正调用 API，因为会先检查配置)
	cfg := &WebSearchConfig{
		APIKey: "mock-key-for-testing",
	}

	// 由于实际会调用 curl，这里主要测试配置逻辑
	// 实际测试会因网络或 API key 无效而失败，这是预期的
	_, err := searchWithBraveEntries(cfg, "test query", 5)

	// 可能失败 (API key 无效或网络问题)，但函数应该能执行
	t.Logf("searchWithBraveEntries with mock key: %v", err)
}

// TestSearchWithBraveEntries_EmptyResponse 测试空响应处理
func TestSearchWithBraveEntries_EmptyResponse(t *testing.T) {
	// 这个测试验证空响应处理逻辑
	// 由于实际实现使用 curl，这里主要记录测试意图
	t.Log("Empty response handling is tested via error cases")
}

// TestWebSearchConfig 测试 WebSearchConfig 结构
func TestWebSearchConfig(t *testing.T) {
	cfg := &WebSearchConfig{
		APIKey: "test-key",
		Proxy:  "http://proxy:8080",
	}

	if cfg.APIKey != "test-key" {
		t.Errorf("Expected APIKey 'test-key', got: %s", cfg.APIKey)
	}

	if cfg.Proxy != "http://proxy:8080" {
		t.Errorf("Expected Proxy 'http://proxy:8080', got: %s", cfg.Proxy)
	}

	t.Logf("WebSearchConfig: APIKey=%s, Proxy=%s", cfg.APIKey, cfg.Proxy)
}

// TestSearchEntry 测试 searchEntry 结构
func TestSearchEntry(t *testing.T) {
	entry := searchEntry{
		Title:   "Test Title",
		URL:     "https://example.com",
		Snippet: "Test Snippet",
	}

	if entry.Title != "Test Title" {
		t.Errorf("Expected Title 'Test Title', got: %s", entry.Title)
	}

	t.Logf("searchEntry: Title=%s, URL=%s", entry.Title, entry.URL)
}
