package tool

import (
	"fmt"

	"github.com/yurika0211/luckyagent-tools/multimodal"
)

// ImageTool inspects or creates images through one entry.
func ImageTool(processor *multimodal.Processor, defaultProvider string, generator multimodal.ImageGenerator, defaults ImageGenerationDefaults) *Tool {
	analyze := ImageAnalyzeTool(processor, defaultProvider)
	read := ImageReadTool()
	generate := ImageGenerateTool(generator, defaults)
	return &Tool{
		Name:        "image",
		Description: "Inspect or create an image. action=read attaches the image for you to inspect directly; action=analyze sends it to an image model; action=generate creates an image and requires approval.",
		Category:    CatBuiltin,
		Source:      "builtin",
		Permission:  PermAuto,
		PermissionFor: func(args map[string]any) PermissionLevel {
			if toolAction(args) == "generate" {
				return PermApprove
			}
			return PermAuto
		},
		Parameters: mergedToolParams(map[string]Param{
			"action": {Type: "string", Description: "Action: read, analyze, or generate.", Required: true},
		}, analyze.Parameters, read.Parameters, generate.Parameters),
		Handler: func(args map[string]any) (string, error) {
			result, err := dispatchImage(args, analyze, read, generate)
			return result.Output, err
		},
		DetailedHandler: func(args map[string]any) (ToolCallResult, error) {
			return dispatchImage(args, analyze, read, generate)
		},
	}
}

func dispatchImage(args map[string]any, analyze, read, generate *Tool) (ToolCallResult, error) {
	var selected *Tool
	switch toolAction(args) {
	case "analyze":
		selected = analyze
	case "read":
		selected = read
	case "generate":
		selected = generate
	default:
		return ToolCallResult{}, fmt.Errorf("invalid image action %q (use read, analyze, or generate)", args["action"])
	}
	if selected.DetailedHandler != nil {
		return selected.DetailedHandler(args)
	}
	out, err := selected.Handler(args)
	return ToolCallResult{Output: out}, err
}

// ImageReadTool returns image content to the current model without invoking
// another model. The agent exposes it only on turns using primary vision.
func ImageReadTool() *Tool {
	t := ImageAnalyzeTool(nil, "")
	t.Name = "image_read"
	t.Description = "Load an image for you to inspect directly. Accepts one of path, url, or base64_data. No external image analysis is performed."
	delete(t.Parameters, "provider")
	delete(t.Parameters, "format")
	t.Handler = func(map[string]any) (string, error) {
		return "", fmt.Errorf("image_read requires structured tool result support")
	}
	t.DetailedHandler = func(args map[string]any) (ToolCallResult, error) {
		opts, err := parseImageAnalyzeOptions(args)
		if err != nil {
			return ToolCallResult{}, err
		}
		input, err := buildImageAnalyzeInput(opts)
		if err != nil {
			return ToolCallResult{}, err
		}
		if input.Modality != multimodal.ModalityImage {
			return ToolCallResult{}, fmt.Errorf("image_read requires an image; use document_read for documents")
		}
		return ToolCallResult{Output: "Image loaded. Inspect the image attached to this tool result.", Observations: []Observation{{
			Kind: "image", FilePath: input.FilePath, ImageURL: input.URL, ImageData: input.Data, MimeType: input.MimeType,
		}}}, nil
	}
	return t
}
