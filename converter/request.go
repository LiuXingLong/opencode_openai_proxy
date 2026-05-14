package converter

import (
	"encoding/json"
	"fmt"
)

type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type ChatCompletionsRequest struct {
	Model            string          `json:"model"`
	Messages         []Message       `json:"messages"`
	Temperature      *float64        `json:"temperature,omitempty"`
	MaxTokens        *int            `json:"max_tokens,omitempty"`
	TopP             *float64        `json:"top_p,omitempty"`
	Stop             json.RawMessage `json:"stop,omitempty"`
	FrequencyPenalty *float64        `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64        `json:"presence_penalty,omitempty"`
	Tools            json.RawMessage `json:"tools,omitempty"`
	ToolChoice       json.RawMessage `json:"tool_choice,omitempty"`
	Stream           bool            `json:"stream,omitempty"`
	StreamOptions    json.RawMessage `json:"stream_options,omitempty"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
	ResponseFormat   json.RawMessage `json:"response_format,omitempty"`
}

type responsesRequest struct {
	Model            string          `json:"model"`
	Input            json.RawMessage `json:"input"`
	Instructions     string          `json:"instructions,omitempty"`
	Temperature      *float64        `json:"temperature,omitempty"`
	MaxOutputTokens  *int            `json:"max_output_tokens,omitempty"`
	TopP             *float64        `json:"top_p,omitempty"`
	Stop             json.RawMessage `json:"stop,omitempty"`
	FrequencyPenalty *float64        `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64        `json:"presence_penalty,omitempty"`
	Tools            json.RawMessage `json:"tools,omitempty"`
	ToolChoice       json.RawMessage `json:"tool_choice,omitempty"`
	Stream           bool            `json:"stream,omitempty"`
	StreamOptions    json.RawMessage `json:"stream_options,omitempty"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
	Text             json.RawMessage `json:"text,omitempty"`
}

type inputItem struct {
	Type    string          `json:"type,omitempty"`
	Role    string          `json:"role,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
}

type contentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

func ConvertRequest(raw []byte) ([]byte, error) {
	var req responsesRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("unmarshal responses request: %w", err)
	}

	chat := ChatCompletionsRequest{
		Model:            req.Model,
		Temperature:      req.Temperature,
		MaxTokens:        req.MaxOutputTokens,
		TopP:             req.TopP,
		Stop:             req.Stop,
		FrequencyPenalty: req.FrequencyPenalty,
		PresencePenalty:  req.PresencePenalty,
		Tools:            req.Tools,
		ToolChoice:       req.ToolChoice,
		Stream:           req.Stream,
		StreamOptions:    req.StreamOptions,
		Metadata:         req.Metadata,
	}

	if req.Instructions != "" {
		chat.Messages = append(chat.Messages, Message{
			Role:    "system",
			Content: json.RawMessage(`"` + jsonEscape(req.Instructions) + `"`),
		})
	}

	msgs, err := convertInput(req.Input)
	if err != nil {
		return nil, fmt.Errorf("convert input: %w", err)
	}
	chat.Messages = append(chat.Messages, msgs...)

	if req.Text != nil {
		var text struct {
			Format json.RawMessage `json:"format"`
		}
		if err := json.Unmarshal(req.Text, &text); err == nil && text.Format != nil {
			chat.ResponseFormat = text.Format
		}
	}

	return json.Marshal(chat)
}

func convertInput(raw json.RawMessage) ([]Message, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return []Message{{Role: "user", Content: mustJSON(s)}}, nil
	}

	if raw[0] != '[' {
		return nil, fmt.Errorf("input must be a string or an array")
	}

	var items []inputItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}

	var msgs []Message
	for _, item := range items {
		if item.Type != "" && item.Type != "message" {
			continue
		}
		role := item.Role
		switch role {
		case "developer":
			role = "system"
		case "user", "assistant", "system":
		default:
			continue
		}
		content := extractContent(item.Content)
		msgs = append(msgs, Message{Role: role, Content: content})
	}
	return msgs, nil
}

func extractContent(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return mustJSON("")
	}

	if raw[0] == '"' {
		return raw
	}

	if raw[0] == '[' {
		var parts []contentPart
		if err := json.Unmarshal(raw, &parts); err != nil {
			return mustJSON("")
		}
		var text string
		for _, p := range parts {
			if p.Type == "input_text" || p.Type == "text" {
				text += p.Text
			}
		}
		return mustJSON(text)
	}

	return mustJSON("")
}

func mustJSON(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}
