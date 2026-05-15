package events

import (
	"encoding/json"
	"testing"
)

func TestMessageContent_UnmarshalArray(t *testing.T) {
	raw := []byte(`{"role":"assistant","content":[{"type":"text","text":"hi"},{"type":"tool_use","name":"Bash","id":"x"}]}`)
	var m MessageContent
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.Role != "assistant" || len(m.Content) != 2 {
		t.Fatalf("unexpected: %+v", m)
	}
	if m.Content[0].Type != "text" || m.Content[0].Text != "hi" {
		t.Fatalf("text block: %+v", m.Content[0])
	}
	if m.Content[1].Type != "tool_use" || m.Content[1].Name != "Bash" {
		t.Fatalf("tool_use block: %+v", m.Content[1])
	}
}

func TestMessageContent_UnmarshalString(t *testing.T) {
	raw := []byte(`{"role":"user","content":"hola mundo"}`)
	var m MessageContent
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.Role != "user" {
		t.Fatalf("role: %q", m.Role)
	}
	if len(m.Content) != 1 || m.Content[0].Type != "text" || m.Content[0].Text != "hola mundo" {
		t.Fatalf("content normalization: %+v", m.Content)
	}
}

func TestMessageContent_UnmarshalNullOrMissing(t *testing.T) {
	cases := [][]byte{
		[]byte(`{"role":"user"}`),
		[]byte(`{"role":"user","content":null}`),
	}
	for _, c := range cases {
		var m MessageContent
		if err := json.Unmarshal(c, &m); err != nil {
			t.Fatalf("%s: %v", c, err)
		}
		if m.Content != nil {
			t.Fatalf("%s: expected nil content, got %+v", c, m.Content)
		}
	}
}

func TestMessageContent_UnmarshalJSON_ExtractsModelAndUsage(t *testing.T) {
	raw := []byte(`{
		"role": "assistant",
		"model": "claude-opus-4-7",
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50,
			"cache_creation_input_tokens": 20,
			"cache_read_input_tokens": 1000
		},
		"content": [{"type": "text", "text": "hi"}]
	}`)
	var m MessageContent
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.Model != "claude-opus-4-7" {
		t.Errorf("Model = %q, want claude-opus-4-7", m.Model)
	}
	if m.Usage == nil {
		t.Fatal("Usage = nil")
	}
	if m.Usage.InputTokens != 100 || m.Usage.OutputTokens != 50 ||
		m.Usage.CacheCreationInputTokens != 20 || m.Usage.CacheReadInputTokens != 1000 {
		t.Errorf("Usage = %+v", *m.Usage)
	}
	if len(m.Content) != 1 || m.Content[0].Text != "hi" {
		t.Errorf("Content = %+v", m.Content)
	}
}

func TestContentBlock_UnmarshalJSON_ParsesIsError(t *testing.T) {
	raw := []byte(`{"type":"tool_result","tool_use_id":"x","is_error":true}`)
	var b ContentBlock
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	if !b.IsError {
		t.Error("IsError = false, want true")
	}
}

func TestRawLine_WithStringUserContent(t *testing.T) {
	raw := []byte(`{"type":"user","message":{"role":"user","content":"agregale soporte para m4a"}}`)
	var line RawLine
	if err := json.Unmarshal(raw, &line); err != nil {
		t.Fatal(err)
	}
	if line.Message == nil || len(line.Message.Content) != 1 || line.Message.Content[0].Text != "agregale soporte para m4a" {
		t.Fatalf("unexpected: %+v", line.Message)
	}
}
