package tool

import (
	"reflect"
	"testing"
)

func TestOpenAIRequiredParameterNamesAreSorted(t *testing.T) {
	tool := &Tool{Name: "stable", Parameters: map[string]Param{
		"z": {Type: "string", Required: true},
		"a": {Type: "string", Required: true},
	}}
	for i := 0; i < 20; i++ {
		parameters := tool.ToOpenAIFormat()["function"].(map[string]any)["parameters"].(map[string]any)
		if got := parameters["required"]; !reflect.DeepEqual(got, []string{"a", "z"}) {
			t.Fatalf("required = %#v", got)
		}
	}
}
