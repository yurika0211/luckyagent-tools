package tool

import "testing"

func TestBuiltinToolServiceRegistersFileSend(t *testing.T) {
	r := NewRegistry()
	NewBuiltinToolService(nil, nil, "", nil, nil, ImageGenerationDefaults{}, nil, TTSDefaults{}).RegisterTools(r)
	tool, ok := r.Get("file_send")
	if !ok {
		t.Fatal("BuiltinToolService did not register file_send")
	}
	if tool.Permission != PermApprove || tool.ParallelSafe || !tool.ShellAware {
		t.Fatalf("file_send flags: perm=%v parallel=%v shell=%v", tool.Permission, tool.ParallelSafe, tool.ShellAware)
	}
}
