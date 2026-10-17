package tool_test

import (
	"fmt"

	tool "github.com/yurika0211/luckyagent-tools"
)

func ExampleRegistry() {
	registry := tool.NewRegistry()
	registry.Register(&tool.Tool{
		Name:       "echo",
		Permission: tool.PermAuto,
		Parameters: map[string]tool.Param{"text": {Type: "string", Required: true}},
		Handler: func(args map[string]any) (string, error) {
			return fmt.Sprint(args["text"]), nil
		},
	})
	output, err := registry.Call("echo", map[string]any{"text": "hello"})
	if err != nil {
		panic(err)
	}
	fmt.Println(output)
	// Output: hello
}
