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
	CallID  string          `json:"call_id,omitempty"`
	Output  string          `json:"output,omitempty"`
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
		Tools:            convertTools(req.Tools),
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
		if item.Type == "function_call_output" {
			output := item.Output
			if output == "" {
				continue
			}
			text := "工具执行结果:\n" + output
			msgs = append(msgs, Message{
				Role:    "user",
				Content: mustJSON(text),
			})
			continue
		}
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

var builtinToolMapping = map[string]map[string]interface{}{
	"web_search": {
		"name":        "web_search",
		"description": "Search the web for information",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The search query",
				},
			},
			"required": []string{"query"},
		},
	},
}

func convertTools(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || raw[0] != '[' {
		return raw
	}

	var tools []json.RawMessage
	if err := json.Unmarshal(raw, &tools); err != nil {
		return raw
	}

	var converted []json.RawMessage
	for _, t := range tools {
		var toolMap map[string]interface{}
		if err := json.Unmarshal(t, &toolMap); err != nil {
			continue
		}

		toolType, _ := toolMap["type"].(string)
		switch toolType {
		case "function":
			funcDef := make(map[string]interface{})
			for k, v := range toolMap {
				if k != "type" {
					funcDef[k] = v
				}
			}
			chatTool := map[string]interface{}{
				"type":     "function",
				"function": funcDef,
			}
			converted = append(converted, mustJSON(chatTool))
		default:
			mapping, ok := builtinToolMapping[toolType]
			if !ok {
				mapping = map[string]interface{}{
					"name":        toolType,
					"description": "Execute a " + toolType + " tool",
					"parameters": map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{},
					},
				}
			}
			chatTool := map[string]interface{}{
				"type":     "function",
				"function": mapping,
			}
			converted = append(converted, mustJSON(chatTool))
		}
	}

	if len(converted) == 0 {
		return nil
	}

	b, _ := json.Marshal(converted)
	return b
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}
