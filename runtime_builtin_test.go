package tool

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/yurika0211/luckyagent-tools/multimodal"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type namedImageTestProvider struct{}

type fakeImageGenerator struct {
	lastReq multimodal.ImageGenerationRequest
	result  *multimodal.ImageGenerationResult
	calls   int
}

type fakeSpeechSynthesizer struct {
	lastReq multimodal.SpeechSynthesisRequest
	result  *multimodal.SpeechSynthesisResult
	calls   int
}

func testLuckyAgentWorkspace(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return filepath.Join(home, ".luckyagent", "workspace")
}

func (g *fakeImageGenerator) Name() string { return "fake-image-generator" }

func (g *fakeImageGenerator) GenerateImage(ctx context.Context, req multimodal.ImageGenerationRequest) (*multimodal.ImageGenerationResult, error) {
	g.calls++
	g.lastReq = req
	if g.result != nil {
		return g.result, nil
	}
	return &multimodal.ImageGenerationResult{
		Provider: "fake-image-generator",
		Model:    "fake-model",
		Images: []multimodal.GeneratedImage{
			{Data: []byte("png-bytes"), MimeType: "image/png"},
		},
	}, nil
}

func (s *fakeSpeechSynthesizer) Name() string { return "fake-speech-synthesizer" }

func (s *fakeSpeechSynthesizer) SynthesizeSpeech(ctx context.Context, req multimodal.SpeechSynthesisRequest) (*multimodal.SpeechSynthesisResult, error) {
	s.calls++
	s.lastReq = req
	if s.result != nil {
		return s.result, nil
	}
	return &multimodal.SpeechSynthesisResult{
		Provider: "fake-speech-synthesizer",
		Model:    "fake-tts-model",
		Voice:    "fake-voice",
		Audio:    []byte("mp3-bytes"),
		MimeType: "audio/mpeg",
	}, nil
}

func (namedImageTestProvider) Name() string { return "named-image-provider" }

func (namedImageTestProvider) SupportedModalities() []multimodal.Modality {
	return []multimodal.Modality{multimodal.ModalityImage}
}

func (namedImageTestProvider) Analyze(ctx context.Context, input *multimodal.Input) (*multimodal.AnalysisResult, error) {
	return &multimodal.AnalysisResult{
		InputID:    input.ID,
		Modality:   input.Modality,
		Text:       "named provider text",
		Summary:    "named provider summary",
		Labels:     []string{"named"},
		Confidence: 0.99,
		Metadata: map[string]string{
			"source": "named-provider",
		},
	}, nil
}

func (namedImageTestProvider) AnalyzeStream(ctx context.Context, input *multimodal.Input) (<-chan multimodal.StreamChunk, error) {
	ch := make(chan multimodal.StreamChunk, 1)
	close(ch)
	return ch, nil
}

func (namedImageTestProvider) Validate() error { return nil }

func TestBuiltinToolsRegistration(t *testing.T) {
	r := NewRegistry()
	RegisterRuntimeBuiltinTools(r)

	expected := []string{"terminal", "file_read", "document_read", "file_write", "file_mkdir", "file_move", "file_delete", "file_patch", "file_list", "web_search", "web_fetch", "opencli", "current_time", "calculate", "image", "text_to_speech", "data", "http_request", "memory", "rag_search", "rag_index"}
	for _, name := range expected {
		tool, ok := r.Get(name)
		if !ok {
			t.Errorf("builtin tool %s not registered", name)
			continue
		}
		if tool.Category != CatBuiltin {
			t.Errorf("expected CatBuiltin for %s, got %s", name, tool.Category)
		}
		if tool.Source != "builtin" {
			t.Errorf("expected source=builtin for %s, got %s", name, tool.Source)
		}
	}

	if r.Count() != len(expected) {
		t.Errorf("expected %d builtin tools, got %d", len(expected), r.Count())
	}

	visible := r.ListModelVisible()
	foundTerminal := false
	for _, tool := range visible {
		if tool.Name == "terminal" {
			foundTerminal = true
		}
	}
	if !foundTerminal {
		t.Error("expected terminal to be model-visible")
	}
}

func TestImageAnalyzeTool(t *testing.T) {
	processor := multimodal.NewProcessor()
	if err := processor.RegisterProvider(multimodal.NewLocalProvider(
		multimodal.ModalityImage,
		multimodal.ModalityDocument,
	), true); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	r := NewRegistry()
	RegisterRuntimeBuiltinTools(r, processor)

	result, err := r.Call("image", map[string]any{
		"action":      "analyze",
		"base64_data": "ZmFrZS1pbWFnZS1ieXRlcw==",
		"mime_type":   "image/png",
	})
	if err != nil {
		t.Fatalf("image analyze call: %v", err)
	}
	if !strings.Contains(result, "Modality: image") {
		t.Fatalf("expected image modality, got %q", result)
	}
	if !strings.Contains(result, "Summary:") {
		t.Fatalf("expected summary, got %q", result)
	}
}

func TestImageAnalyzeToolUsesConfiguredDefaultProvider(t *testing.T) {
	processor := multimodal.NewProcessor()
	if err := processor.RegisterProvider(multimodal.NewLocalProvider(
		multimodal.ModalityImage,
	), true); err != nil {
		t.Fatalf("register local provider: %v", err)
	}
	if err := processor.RegisterProvider(namedImageTestProvider{}, false); err != nil {
		t.Fatalf("register named provider: %v", err)
	}

	r := NewRegistry()
	r.Register(ImageAnalyzeTool(processor, "named-image-provider"))

	result, err := r.Call("image_analyze", map[string]any{
		"base64_data": "ZmFrZS1pbWFnZS1ieXRlcw==",
		"mime_type":   "image/png",
	})
	if err != nil {
		t.Fatalf("image_analyze call: %v", err)
	}
	if !strings.Contains(result, "named provider summary") {
		t.Fatalf("expected configured provider output, got %q", result)
	}
}

func TestImageAnalyzeToolRejectsMultipleInputSources(t *testing.T) {
	processor := multimodal.NewProcessor()
	if err := processor.RegisterProvider(multimodal.NewLocalProvider(multimodal.ModalityImage), true); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	r := NewRegistry()
	r.Register(ImageAnalyzeTool(processor, ""))

	_, err := r.Call("image_analyze", map[string]any{
		"path":        "/tmp/example.png",
		"base64_data": "ZmFrZS1pbWFnZS1ieXRlcw==",
		"mime_type":   "image/png",
	})
	if err == nil {
		t.Fatal("expected multiple input sources to be rejected")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImageAnalyzeToolRejectsPrivateURL(t *testing.T) {
	processor := multimodal.NewProcessor()
	if err := processor.RegisterProvider(multimodal.NewLocalProvider(multimodal.ModalityImage), true); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	r := NewRegistry()
	r.Register(ImageAnalyzeTool(processor, ""))

	_, err := r.Call("image_analyze", map[string]any{
		"url":       "http://127.0.0.1/image.png",
		"mime_type": "image/png",
	})
	if err == nil {
		t.Fatal("expected private url to be rejected")
	}
	if !strings.Contains(err.Error(), "url validation failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImageAnalyzeToolRejectsUnsupportedMIME(t *testing.T) {
	processor := multimodal.NewProcessor()
	if err := processor.RegisterProvider(multimodal.NewLocalProvider(multimodal.ModalityImage), true); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	r := NewRegistry()
	r.Register(ImageAnalyzeTool(processor, ""))

	_, err := r.Call("image_analyze", map[string]any{
		"base64_data": base64.StdEncoding.EncodeToString([]byte("not an image")),
		"mime_type":   "text/plain",
	})
	if err == nil {
		t.Fatal("expected unsupported MIME to be rejected")
	}
	if !strings.Contains(err.Error(), "unsupported image_analyze MIME type") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImageAnalyzeToolSupportsJSONFormat(t *testing.T) {
	processor := multimodal.NewProcessor()
	if err := processor.RegisterProvider(multimodal.NewLocalProvider(multimodal.ModalityImage), true); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	r := NewRegistry()
	r.Register(ImageAnalyzeTool(processor, ""))

	result, err := r.Call("image_analyze", map[string]any{
		"base64_data": "ZmFrZS1pbWFnZS1ieXRlcw==",
		"mime_type":   "image/png",
		"format":      "json",
	})
	if err != nil {
		t.Fatalf("image_analyze call: %v", err)
	}
	var payload struct {
		Available bool   `json:"available"`
		Modality  string `json:"modality"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("unmarshal tool output: %v", err)
	}
	if !payload.Available || payload.Modality != "image" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestImageGenerateToolSavesOutputAndSupportsImageToImage(t *testing.T) {
	gen := &fakeImageGenerator{
		result: &multimodal.ImageGenerationResult{
			Provider:      "fake-image-generator",
			Model:         "gpt-image-1.5",
			RevisedPrompt: "refined prompt",
			Images: []multimodal.GeneratedImage{
				{Data: []byte("generated-image-bytes"), MimeType: "image/png"},
			},
		},
	}

	tmpDir := t.TempDir()
	outputDir := filepath.Join(testLuckyAgentWorkspace(t), "generated-images-test")
	inputPath := filepath.Join(tmpDir, "input.png")
	if err := os.WriteFile(inputPath, []byte("input-image-bytes"), 0o644); err != nil {
		t.Fatalf("write input image: %v", err)
	}

	r := NewRegistry()
	r.Register(ImageGenerateTool(gen, ImageGenerationDefaults{}))

	result, err := r.CallWithShellContext("image_generate", map[string]any{
		"prompt":        "turn this into a watercolor postcard",
		"input_path":    inputPath,
		"output_dir":    outputDir,
		"count":         1,
		"output_format": "png",
	}, &ShellContext{Cwd: tmpDir})
	if err != nil {
		t.Fatalf("image_generate call: %v", err)
	}

	if gen.lastReq.Prompt != "turn this into a watercolor postcard" {
		t.Fatalf("unexpected prompt: %q", gen.lastReq.Prompt)
	}
	if len(gen.lastReq.InputImages) != 1 {
		t.Fatalf("expected 1 input image, got %d", len(gen.lastReq.InputImages))
	}
	if string(gen.lastReq.InputImages[0].Data) != "input-image-bytes" {
		t.Fatalf("unexpected input image bytes: %q", string(gen.lastReq.InputImages[0].Data))
	}

	var payload struct {
		Paths []string `json:"paths"`
		Model string   `json:"model"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("unmarshal tool output: %v", err)
	}
	if payload.Model != "gpt-image-1.5" {
		t.Fatalf("unexpected model in output: %q", payload.Model)
	}
	if len(payload.Paths) != 1 {
		t.Fatalf("expected 1 saved path, got %d", len(payload.Paths))
	}
	written, err := os.ReadFile(payload.Paths[0])
	if err != nil {
		t.Fatalf("read generated output: %v", err)
	}
	if string(written) != "generated-image-bytes" {
		t.Fatalf("unexpected written bytes: %q", string(written))
	}
}

func TestImageGenerateToolUsesConfiguredDefaults(t *testing.T) {
	gen := &fakeImageGenerator{}
	r := NewRegistry()
	r.Register(ImageGenerateTool(gen, ImageGenerationDefaults{
		Model:        "gpt-image-1.5",
		Size:         "1536x1024",
		Quality:      "high",
		Background:   "transparent",
		OutputFormat: "webp",
	}))

	tmpDir := t.TempDir()
	outputDir := filepath.Join(testLuckyAgentWorkspace(t), "default-images")
	_, err := r.CallWithShellContext("image_generate", map[string]any{
		"prompt":     "a minimal poster",
		"output_dir": outputDir,
	}, &ShellContext{Cwd: tmpDir})
	if err != nil {
		t.Fatalf("image_generate call: %v", err)
	}

	if gen.lastReq.Model != "gpt-image-1.5" {
		t.Fatalf("expected default model, got %q", gen.lastReq.Model)
	}
	if gen.lastReq.Size != "1536x1024" {
		t.Fatalf("expected default size, got %q", gen.lastReq.Size)
	}
	if gen.lastReq.Quality != "high" {
		t.Fatalf("expected default quality, got %q", gen.lastReq.Quality)
	}
	if gen.lastReq.Background != "transparent" {
		t.Fatalf("expected default background, got %q", gen.lastReq.Background)
	}
	if gen.lastReq.OutputFormat != "webp" {
		t.Fatalf("expected default output format, got %q", gen.lastReq.OutputFormat)
	}
}

func TestImageGenerateToolDefaultsOutputToWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	gen := &fakeImageGenerator{
		result: &multimodal.ImageGenerationResult{
			Provider: "fake-image-generator",
			Model:    "gpt-image-1.5",
			Images: []multimodal.GeneratedImage{
				{Data: []byte("generated-image-bytes"), MimeType: "image/png"},
			},
		},
	}
	r := NewRegistry()
	r.Register(ImageGenerateTool(gen, ImageGenerationDefaults{}))

	result, err := r.Call("image_generate", map[string]any{"prompt": "a workspace image"})
	if err != nil {
		t.Fatalf("image_generate call: %v", err)
	}

	var payload struct {
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("unmarshal tool output: %v", err)
	}
	if len(payload.Paths) != 1 {
		t.Fatalf("expected 1 saved path, got %d", len(payload.Paths))
	}
	wantPrefix := filepath.Join(home, ".luckyagent", "workspace", "generated-images")
	if !strings.HasPrefix(payload.Paths[0], wantPrefix+string(os.PathSeparator)) {
		t.Fatalf("expected generated image under %q, got %q", wantPrefix, payload.Paths[0])
	}
}

func TestImageGenerateToolSupportsMultipleInputPaths(t *testing.T) {
	gen := &fakeImageGenerator{
		result: &multimodal.ImageGenerationResult{
			Provider: "fake-image-generator",
			Model:    "gpt-image-1.5",
			Images: []multimodal.GeneratedImage{
				{Data: []byte("generated-image-bytes"), MimeType: "image/png"},
			},
		},
	}

	tmpDir := t.TempDir()
	outputDir := filepath.Join(testLuckyAgentWorkspace(t), "multi-input-images")
	inputPath1 := filepath.Join(tmpDir, "input-1.png")
	inputPath2 := filepath.Join(tmpDir, "input-2.png")
	if err := os.WriteFile(inputPath1, []byte("image-one"), 0o644); err != nil {
		t.Fatalf("write input 1: %v", err)
	}
	if err := os.WriteFile(inputPath2, []byte("image-two"), 0o644); err != nil {
		t.Fatalf("write input 2: %v", err)
	}

	r := NewRegistry()
	r.Register(ImageGenerateTool(gen, ImageGenerationDefaults{}))

	_, err := r.CallWithShellContext("image_generate", map[string]any{
		"prompt":      "blend these two references",
		"input_paths": []any{inputPath1, inputPath2},
		"output_dir":  outputDir,
	}, &ShellContext{Cwd: tmpDir})
	if err != nil {
		t.Fatalf("image_generate call: %v", err)
	}
	if len(gen.lastReq.InputImages) != 2 {
		t.Fatalf("expected 2 input images, got %d", len(gen.lastReq.InputImages))
	}
	if string(gen.lastReq.InputImages[0].Data) != "image-one" || string(gen.lastReq.InputImages[1].Data) != "image-two" {
		t.Fatalf("unexpected input bytes: %#v", gen.lastReq.InputImages)
	}
}

func TestImageGenerateToolRejectsOutputOutsideWorkspace(t *testing.T) {
	workspace := testLuckyAgentWorkspace(t)
	gen := &fakeImageGenerator{
		result: &multimodal.ImageGenerationResult{
			Provider: "fake-image-generator",
			Model:    "gpt-image-1.5",
			Images: []multimodal.GeneratedImage{
				{Data: []byte("generated-image-bytes"), MimeType: "image/png"},
			},
		},
	}
	r := NewRegistry()
	r.Register(ImageGenerateTool(gen, ImageGenerationDefaults{}))

	_, err := r.Call("image_generate", map[string]any{
		"prompt":     "outside workspace",
		"output_dir": filepath.Join(filepath.Dir(workspace), "data", "images"),
	})
	if err == nil {
		t.Fatal("expected output_dir outside workspace to be rejected")
	}
	if !strings.Contains(err.Error(), "outside workspace") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImageGenerateToolRejectsFilenamePrefixEscapingWorkspace(t *testing.T) {
	workspace := testLuckyAgentWorkspace(t)
	gen := &fakeImageGenerator{
		result: &multimodal.ImageGenerationResult{
			Provider: "fake-image-generator",
			Model:    "gpt-image-1.5",
			Images: []multimodal.GeneratedImage{
				{Data: []byte("generated-image-bytes"), MimeType: "image/png"},
			},
		},
	}
	r := NewRegistry()
	r.Register(ImageGenerateTool(gen, ImageGenerationDefaults{}))

	_, err := r.Call("image_generate", map[string]any{
		"prompt":          "escape workspace",
		"output_dir":      filepath.Join(workspace, "generated-images"),
		"filename_prefix": "../../outside",
	})
	if err == nil {
		t.Fatal("expected escaping filename_prefix to be rejected")
	}
	if !strings.Contains(err.Error(), "filename_prefix") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImageGenerateToolDryRunReturnsPlanWithoutCallingProvider(t *testing.T) {
	workspace := testLuckyAgentWorkspace(t)
	gen := &fakeImageGenerator{}
	r := NewRegistry()
	r.Register(ImageGenerateTool(gen, ImageGenerationDefaults{
		Model:        "gpt-image-1.5",
		Size:         "1024x1024",
		Quality:      "high",
		OutputFormat: "webp",
	}))

	result, err := r.Call("image_generate", map[string]any{
		"prompt":     "plan an image",
		"dry_run":    true,
		"count":      2,
		"output_dir": filepath.Join(workspace, "planned-images"),
	})
	if err != nil {
		t.Fatalf("image_generate dry_run: %v", err)
	}
	if gen.calls != 0 {
		t.Fatalf("expected provider not to be called, got %d calls", gen.calls)
	}
	var payload struct {
		DryRun        bool     `json:"dry_run"`
		Provider      string   `json:"provider"`
		Model         string   `json:"model"`
		Count         int      `json:"count"`
		OutputTargets []string `json:"output_targets"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("unmarshal dry_run payload: %v", err)
	}
	if !payload.DryRun || payload.Provider != "fake-image-generator" || payload.Model != "gpt-image-1.5" || payload.Count != 2 {
		t.Fatalf("unexpected dry_run payload: %+v", payload)
	}
	if len(payload.OutputTargets) != 2 {
		t.Fatalf("expected 2 output targets, got %d", len(payload.OutputTargets))
	}
}

func TestImageGenerateToolRejectsTooManyInputs(t *testing.T) {
	gen := &fakeImageGenerator{}
	r := NewRegistry()
	r.Register(ImageGenerateTool(gen, ImageGenerationDefaults{}))

	inputs := make([]any, 0, maxImageGenerateInputs+1)
	for i := 0; i < maxImageGenerateInputs+1; i++ {
		inputs = append(inputs, base64.StdEncoding.EncodeToString([]byte("fake")))
	}
	_, err := r.Call("image_generate", map[string]any{
		"prompt":              "too many references",
		"input_base64_datas":  inputs,
		"input_mime_types":    []any{"image/png", "image/png", "image/png", "image/png", "image/png", "image/png", "image/png", "image/png", "image/png"},
		"filename_prefix":     "too-many",
		"output_compression":  0,
		"output_format":       "png",
		"count":               1,
		"allow_custom_format": false,
	})
	if err == nil {
		t.Fatal("expected too many inputs to be rejected")
	}
	if !strings.Contains(err.Error(), "at most") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImageGenerateToolRejectsUnsupportedInputMIME(t *testing.T) {
	gen := &fakeImageGenerator{}
	r := NewRegistry()
	r.Register(ImageGenerateTool(gen, ImageGenerationDefaults{}))

	_, err := r.Call("image_generate", map[string]any{
		"prompt":            "bad input",
		"input_base64_data": base64.StdEncoding.EncodeToString([]byte("not an image")),
		"input_mime_type":   "text/plain",
	})
	if err == nil {
		t.Fatal("expected unsupported input MIME to be rejected")
	}
	if !strings.Contains(err.Error(), "unsupported image input MIME type") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImageGenerateToolRejectsExistingOutputPathUnlessOverwrite(t *testing.T) {
	workspace := testLuckyAgentWorkspace(t)
	outputPath := filepath.Join(workspace, "generated-images", "existing.png")
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}
	if err := os.WriteFile(outputPath, []byte("old"), 0o644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}
	gen := &fakeImageGenerator{
		result: &multimodal.ImageGenerationResult{
			Provider: "fake-image-generator",
			Model:    "gpt-image-1.5",
			Images: []multimodal.GeneratedImage{
				{Data: []byte("new"), MimeType: "image/png"},
			},
		},
	}
	r := NewRegistry()
	r.Register(ImageGenerateTool(gen, ImageGenerationDefaults{}))

	_, err := r.Call("image_generate", map[string]any{
		"prompt":      "existing path",
		"output_path": outputPath,
	})
	if err == nil {
		t.Fatal("expected existing output_path to be rejected")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = r.Call("image_generate", map[string]any{
		"prompt":      "overwrite path",
		"output_path": outputPath,
		"overwrite":   true,
	})
	if err != nil {
		t.Fatalf("image_generate overwrite: %v", err)
	}
	written, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(written) != "new" {
		t.Fatalf("expected overwritten bytes, got %q", string(written))
	}
}

func TestImageGenerateToolAllocatesUniquePathForOutputDirConflict(t *testing.T) {
	workspace := testLuckyAgentWorkspace(t)
	outputDir := filepath.Join(workspace, "generated-images")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}
	existing := filepath.Join(outputDir, "generated-image-01.png")
	if err := os.WriteFile(existing, []byte("old"), 0o644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}
	gen := &fakeImageGenerator{
		result: &multimodal.ImageGenerationResult{
			Provider: "fake-image-generator",
			Model:    "gpt-image-1.5",
			Images: []multimodal.GeneratedImage{
				{Data: []byte("new"), MimeType: "image/png"},
			},
		},
	}
	r := NewRegistry()
	r.Register(ImageGenerateTool(gen, ImageGenerationDefaults{}))

	result, err := r.Call("image_generate", map[string]any{
		"prompt":     "avoid conflict",
		"output_dir": outputDir,
	})
	if err != nil {
		t.Fatalf("image_generate call: %v", err)
	}
	var payload struct {
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(payload.Paths) != 1 || payload.Paths[0] == existing {
		t.Fatalf("expected unique path different from %q, got %+v", existing, payload.Paths)
	}
	original, err := os.ReadFile(existing)
	if err != nil {
		t.Fatalf("read existing: %v", err)
	}
	if string(original) != "old" {
		t.Fatalf("existing file was overwritten: %q", string(original))
	}
}

func TestTextToSpeechToolDefaultsOutputToWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	synth := &fakeSpeechSynthesizer{
		result: &multimodal.SpeechSynthesisResult{
			Provider: "fake-speech-synthesizer",
			Model:    "gpt-4o-mini-tts",
			Voice:    "alloy",
			Audio:    []byte("speech-bytes"),
			MimeType: "audio/mpeg",
		},
	}
	r := NewRegistry()
	r.Register(TextToSpeechTool(synth, TTSDefaults{}))

	result, err := r.Call("text_to_speech", map[string]any{"text": "hello workspace"})
	if err != nil {
		t.Fatalf("text_to_speech call: %v", err)
	}

	var payload struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("unmarshal tool output: %v", err)
	}
	wantPrefix := filepath.Join(home, ".luckyagent", "workspace", "generated-audio")
	if !strings.HasPrefix(payload.Path, wantPrefix+string(os.PathSeparator)) {
		t.Fatalf("expected generated audio under %q, got %q", wantPrefix, payload.Path)
	}
}

func TestDownloadImageInputRejectsPrivateURL(t *testing.T) {
	_, _, err := downloadImageInput("http://127.0.0.1/image.png")
	if err == nil {
		t.Fatal("expected private input_url to be rejected")
	}
	if !strings.Contains(err.Error(), "input_url validation failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTextToSpeechToolSavesOutput(t *testing.T) {
	synth := &fakeSpeechSynthesizer{
		result: &multimodal.SpeechSynthesisResult{
			Provider: "fake-speech-synthesizer",
			Model:    "gpt-4o-mini-tts",
			Voice:    "alloy",
			Audio:    []byte("speech-bytes"),
			MimeType: "audio/mpeg",
		},
	}
	tmpDir := t.TempDir()
	outputDir := filepath.Join(testLuckyAgentWorkspace(t), "tts-output")

	r := NewRegistry()
	r.Register(TextToSpeechTool(synth, TTSDefaults{}))

	result, err := r.CallWithShellContext("text_to_speech", map[string]any{
		"text":       "hello from luckyagent",
		"output_dir": outputDir,
	}, &ShellContext{Cwd: tmpDir})
	if err != nil {
		t.Fatalf("text_to_speech call: %v", err)
	}

	if synth.lastReq.Text != "hello from luckyagent" {
		t.Fatalf("unexpected text: %q", synth.lastReq.Text)
	}

	var payload struct {
		Path  string `json:"path"`
		Model string `json:"model"`
		Voice string `json:"voice"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("unmarshal tool output: %v", err)
	}
	if payload.Model != "gpt-4o-mini-tts" || payload.Voice != "alloy" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	written, err := os.ReadFile(payload.Path)
	if err != nil {
		t.Fatalf("read synthesized output: %v", err)
	}
	if string(written) != "speech-bytes" {
		t.Fatalf("unexpected written bytes: %q", string(written))
	}
}

func TestTextToSpeechToolUsesConfiguredDefaults(t *testing.T) {
	synth := &fakeSpeechSynthesizer{}
	r := NewRegistry()
	r.Register(TextToSpeechTool(synth, TTSDefaults{
		Model:  "gpt-4o-mini-tts",
		Voice:  "alloy",
		Format: "wav",
		Speed:  1.25,
	}))

	tmpDir := t.TempDir()
	outputDir := filepath.Join(testLuckyAgentWorkspace(t), "tts-defaults")
	_, err := r.CallWithShellContext("text_to_speech", map[string]any{
		"text":       "test defaults",
		"output_dir": outputDir,
	}, &ShellContext{Cwd: tmpDir})
	if err != nil {
		t.Fatalf("text_to_speech call: %v", err)
	}

	if synth.lastReq.Model != "gpt-4o-mini-tts" {
		t.Fatalf("expected default model, got %q", synth.lastReq.Model)
	}
	if synth.lastReq.Voice != "alloy" {
		t.Fatalf("expected default voice, got %q", synth.lastReq.Voice)
	}
	if synth.lastReq.Format != "wav" {
		t.Fatalf("expected default format, got %q", synth.lastReq.Format)
	}
	if synth.lastReq.Speed != 1.25 {
		t.Fatalf("expected default speed, got %f", synth.lastReq.Speed)
	}
}

func TestTextToSpeechToolGeneratesUniqueDefaultPaths(t *testing.T) {
	synth := &fakeSpeechSynthesizer{
		result: &multimodal.SpeechSynthesisResult{
			Provider: "fake-speech-synthesizer",
			Model:    "gpt-4o-mini-tts",
			Voice:    "alloy",
			Audio:    []byte("speech-bytes"),
			MimeType: "audio/mpeg",
		},
	}
	r := NewRegistry()
	r.Register(TextToSpeechTool(synth, TTSDefaults{}))

	tmpDir := t.TempDir()
	outputDir := filepath.Join(testLuckyAgentWorkspace(t), "tts-unique")
	call := func(text string) string {
		out, err := r.CallWithShellContext("text_to_speech", map[string]any{
			"text":       text,
			"output_dir": outputDir,
		}, &ShellContext{Cwd: tmpDir})
		if err != nil {
			t.Fatalf("text_to_speech call: %v", err)
		}
		var payload struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(out), &payload); err != nil {
			t.Fatalf("unmarshal tool output: %v", err)
		}
		return payload.Path
	}

	path1 := call("first audio")
	path2 := call("second audio")
	if path1 == path2 {
		t.Fatalf("expected unique output paths, got %q and %q", path1, path2)
	}
}

func TestTextToSpeechToolRejectsOutputPathOutsideWorkspace(t *testing.T) {
	workspace := testLuckyAgentWorkspace(t)
	synth := &fakeSpeechSynthesizer{
		result: &multimodal.SpeechSynthesisResult{
			Provider: "fake-speech-synthesizer",
			Model:    "gpt-4o-mini-tts",
			Voice:    "alloy",
			Audio:    []byte("speech-bytes"),
			MimeType: "audio/mpeg",
		},
	}
	r := NewRegistry()
	r.Register(TextToSpeechTool(synth, TTSDefaults{}))

	_, err := r.Call("text_to_speech", map[string]any{
		"text":        "outside workspace",
		"output_path": filepath.Join(filepath.Dir(workspace), "data", "audio.mp3"),
	})
	if err == nil {
		t.Fatal("expected output_path outside workspace to be rejected")
	}
	if !strings.Contains(err.Error(), "outside workspace") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTextToSpeechToolDryRunReturnsPlanWithoutCallingProvider(t *testing.T) {
	workspace := testLuckyAgentWorkspace(t)
	synth := &fakeSpeechSynthesizer{}
	r := NewRegistry()
	r.Register(TextToSpeechTool(synth, TTSDefaults{
		Model:  "gpt-4o-mini-tts",
		Voice:  "alloy",
		Format: "wav",
		Speed:  1.25,
	}))

	result, err := r.Call("text_to_speech", map[string]any{
		"text":        "hello dry run",
		"dry_run":     true,
		"output_path": filepath.Join(workspace, "audio", "hello.wav"),
	})
	if err != nil {
		t.Fatalf("text_to_speech dry_run: %v", err)
	}
	if synth.calls != 0 {
		t.Fatalf("expected provider not to be called, got %d calls", synth.calls)
	}
	var payload struct {
		DryRun       bool    `json:"dry_run"`
		Provider     string  `json:"provider"`
		Model        string  `json:"model"`
		Voice        string  `json:"voice"`
		Format       string  `json:"format"`
		Speed        float64 `json:"speed"`
		TextChars    int     `json:"text_chars"`
		OutputTarget string  `json:"output_target"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("unmarshal dry_run payload: %v", err)
	}
	if !payload.DryRun || payload.Provider != "fake-speech-synthesizer" || payload.Format != "wav" || payload.TextChars != len("hello dry run") {
		t.Fatalf("unexpected dry_run payload: %+v", payload)
	}
	if payload.OutputTarget == "" {
		t.Fatalf("expected output target in dry_run payload: %+v", payload)
	}
}

func TestTextToSpeechToolRejectsLongText(t *testing.T) {
	synth := &fakeSpeechSynthesizer{}
	r := NewRegistry()
	r.Register(TextToSpeechTool(synth, TTSDefaults{}))

	_, err := r.Call("text_to_speech", map[string]any{
		"text": strings.Repeat("x", maxTTSInputChars+1),
	})
	if err == nil {
		t.Fatal("expected long text to be rejected")
	}
	if !strings.Contains(err.Error(), "text exceeds") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTextToSpeechToolValidatesSpeedAndFormat(t *testing.T) {
	synth := &fakeSpeechSynthesizer{}
	r := NewRegistry()
	r.Register(TextToSpeechTool(synth, TTSDefaults{}))

	_, err := r.Call("text_to_speech", map[string]any{
		"text":   "bad speed",
		"speed":  9.0,
		"format": "mp3",
	})
	if err == nil {
		t.Fatal("expected speed to be rejected")
	}
	if !strings.Contains(err.Error(), "speed must be between") {
		t.Fatalf("unexpected speed error: %v", err)
	}

	_, err = r.Call("text_to_speech", map[string]any{
		"text":   "bad format",
		"format": "ogg",
	})
	if err == nil {
		t.Fatal("expected unsupported format to be rejected")
	}
	if !strings.Contains(err.Error(), "unsupported audio format") {
		t.Fatalf("unexpected format error: %v", err)
	}

	_, err = r.Call("text_to_speech", map[string]any{
		"text":                "custom format",
		"format":              "ogg",
		"allow_custom_format": true,
		"dry_run":             true,
	})
	if err != nil {
		t.Fatalf("expected custom format with allow_custom_format to pass: %v", err)
	}
}

func TestTextToSpeechToolRejectsFilenamePrefixEscapingWorkspace(t *testing.T) {
	synth := &fakeSpeechSynthesizer{}
	r := NewRegistry()
	r.Register(TextToSpeechTool(synth, TTSDefaults{}))

	_, err := r.Call("text_to_speech", map[string]any{
		"text":            "bad prefix",
		"filename_prefix": "../audio",
	})
	if err == nil {
		t.Fatal("expected invalid filename_prefix to be rejected")
	}
	if !strings.Contains(err.Error(), "filename_prefix") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTextToSpeechToolRejectsExistingOutputUnlessOverwrite(t *testing.T) {
	workspace := testLuckyAgentWorkspace(t)
	outputPath := filepath.Join(workspace, "generated-audio", "existing.mp3")
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}
	if err := os.WriteFile(outputPath, []byte("old"), 0o644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}
	synth := &fakeSpeechSynthesizer{
		result: &multimodal.SpeechSynthesisResult{
			Provider: "fake-speech-synthesizer",
			Model:    "gpt-4o-mini-tts",
			Voice:    "alloy",
			Audio:    []byte("new"),
			MimeType: "audio/mpeg",
		},
	}
	r := NewRegistry()
	r.Register(TextToSpeechTool(synth, TTSDefaults{}))

	_, err := r.Call("text_to_speech", map[string]any{
		"text":        "existing output",
		"output_path": outputPath,
	})
	if err == nil {
		t.Fatal("expected existing output_path to be rejected")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error: %v", err)
	}
	if synth.calls != 0 {
		t.Fatalf("expected conflict before provider call, got %d calls", synth.calls)
	}

	_, err = r.Call("text_to_speech", map[string]any{
		"text":        "overwrite output",
		"output_path": outputPath,
		"overwrite":   true,
	})
	if err != nil {
		t.Fatalf("text_to_speech overwrite: %v", err)
	}
	written, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(written) != "new" {
		t.Fatalf("expected overwritten bytes, got %q", string(written))
	}
}

func TestToolPermissions(t *testing.T) {
	r := NewRegistry()
	RegisterRuntimeBuiltinTools(r)

	// 只读工具应该是 auto
	readPerm, _ := r.CheckPermission("file_read")
	if readPerm != PermAuto {
		t.Errorf("file_read should be auto, got %s", readPerm)
	}
	docPerm, _ := r.CheckPermission("document_read")
	if docPerm != PermAuto {
		t.Errorf("document_read should be auto, got %s", docPerm)
	}

	// 写操作应该是 approve
	writePerm, _ := r.CheckPermission("file_write")
	if writePerm != PermApprove {
		t.Errorf("file_write should be approve, got %s", writePerm)
	}
	mkdirPerm, _ := r.CheckPermission("file_mkdir")
	if mkdirPerm != PermApprove {
		t.Errorf("file_mkdir should be approve, got %s", mkdirPerm)
	}
	movePerm, _ := r.CheckPermission("file_move")
	if movePerm != PermApprove {
		t.Errorf("file_move should be approve, got %s", movePerm)
	}
	deletePerm, _ := r.CheckPermission("file_delete")
	if deletePerm != PermApprove {
		t.Errorf("file_delete should be approve, got %s", deletePerm)
	}
	patchPerm, _ := r.CheckPermission("file_patch")
	if patchPerm != PermApprove {
		t.Errorf("file_patch should be approve, got %s", patchPerm)
	}

	// shell 应该是 approve
	terminalPerm, _ := r.CheckPermission("terminal")
	if terminalPerm != PermApprove {
		t.Errorf("terminal should be approve, got %s", terminalPerm)
	}

	// current_time 应该是 auto
	timePerm, _ := r.CheckPermission("current_time")
	if timePerm != PermAuto {
		t.Errorf("current_time should be auto, got %s", timePerm)
	}
	calcPerm, _ := r.CheckPermission("calculate")
	if calcPerm != PermAuto {
		t.Errorf("calculate should be auto, got %s", calcPerm)
	}
	dataTool, ok := r.Get("data")
	if !ok {
		t.Fatal("data tool not registered")
	}
	if dataTool.EffectivePermission(map[string]any{"action": "log"}) != PermAuto {
		t.Error("data log should be auto")
	}
	if dataTool.EffectivePermission(map[string]any{"action": "query"}) != PermAuto {
		t.Error("data query should be auto")
	}
	if dataTool.EffectivePermission(map[string]any{"action": "schema"}) != PermAuto {
		t.Error("data schema should be auto")
	}
	if dataTool.EffectivePermission(map[string]any{"action": "sql"}) != PermApprove {
		t.Error("data sql should require approval")
	}
	httpPerm, _ := r.CheckPermission("http_request")
	if httpPerm != PermApprove {
		t.Errorf("http_request should be approve, got %s", httpPerm)
	}
}
