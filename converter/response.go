package converter

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type responsesResponse struct {
	ID        string          `json:"id"`
	Object    string          `json:"object"`
	CreatedAt int64           `json:"created_at"`
	Status    string          `json:"status"`
	Model     string          `json:"model"`
	Output    json.RawMessage `json:"output"`
	Usage     json.RawMessage `json:"usage"`
}

type chatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []choice `json:"choices"`
	Usage   *usage   `json:"usage,omitempty"`
}

type choice struct {
	Index        int             `json:"index"`
	Message      responseMessage `json:"message"`
	FinishReason *string         `json:"finish_reason,omitempty"`
}

type responseMessage struct {
	Role      string          `json:"role"`
	Content   *string         `json:"content,omitempty"`
	ToolCalls json.RawMessage `json:"tool_calls,omitempty"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type responseUsage struct {
	InputTokens   int                  `json:"input_tokens"`
	OutputTokens  int                  `json:"output_tokens"`
	TotalTokens   int                  `json:"total_tokens"`
	OutputDetails *outputTokensDetails `json:"output_tokens_details,omitempty"`
}

type outputTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

const responsesIDPrefix = "resp_"

func ConvertNonStreamingResponse(upstreamBody []byte) ([]byte, error) {
	var chatResp chatCompletionResponse
	if err := json.Unmarshal(upstreamBody, &chatResp); err != nil {
		return nil, fmt.Errorf("unmarshal chat completion response: %w", err)
	}

	resp := responsesResponse{
		ID:        responsesIDPrefix + uuid.New().String(),
		Object:    "response",
		CreatedAt: chatResp.Created,
		Model:     chatResp.Model,
	}

	if len(chatResp.Choices) > 0 {
		ch := chatResp.Choices[0]
		resp.Status = MapFinishReason(ch.FinishReason)
		resp.Output = buildOutput(ch.Message)
	}

	if chatResp.Usage != nil {
		resp.Usage = convertUsage(chatResp.Usage)
	}

	return json.Marshal(resp)
}

func buildOutput(msg responseMessage) json.RawMessage {
	var outputItems []json.RawMessage

	if msg.Content != nil && *msg.Content != "" {
		msgID := "msg_" + uuid.New().String()
		contentItem := map[string]interface{}{
			"type":        "output_text",
			"text":        *msg.Content,
			"annotations": []interface{}{},
		}
		messageItem := map[string]interface{}{
			"id":      msgID,
			"type":    "message",
			"role":    "assistant",
			"content": []interface{}{contentItem},
		}
		outputItems = append(outputItems, mustJSON(messageItem))
	}

	if msg.ToolCalls != nil {
		var toolCalls []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		}
		if err := json.Unmarshal(msg.ToolCalls, &toolCalls); err == nil {
			for _, tc := range toolCalls {
				fcItem := map[string]interface{}{
					"id":        tc.ID,
					"type":      "function_call",
					"call_id":   tc.ID,
					"name":      tc.Function.Name,
					"arguments": tc.Function.Arguments,
					"status":    "completed",
				}
				outputItems = append(outputItems, mustJSON(fcItem))
			}
		}
	}

	if len(outputItems) == 0 {
		return json.RawMessage(`[]`)
	}

	b, _ := json.Marshal(outputItems)
	return b
}

func convertUsage(u *usage) json.RawMessage {
	ru := responseUsage{
		InputTokens:  u.PromptTokens,
		OutputTokens: u.CompletionTokens,
		TotalTokens:  u.TotalTokens,
		OutputDetails: &outputTokensDetails{
			ReasoningTokens: 0,
		},
	}
	b, _ := json.Marshal(ru)
	return b
}

func MapFinishReason(reason *string) string {
	if reason == nil {
		return "in_progress"
	}
	switch *reason {
	case "stop":
		return "completed"
	case "length":
		return "incomplete"
	case "content_filter":
		return "incomplete"
	default:
		return "completed"
	}
}

func BuildStreamCreatedEvent(respID, model string, createdAt int64) string {
	resp := map[string]interface{}{
		"id":         respID,
		"object":     "response",
		"created_at": createdAt,
		"model":      model,
		"status":     "in_progress",
		"output":     []interface{}{},
	}
	data, _ := json.Marshal(map[string]interface{}{
		"type":     "response.created",
		"response": resp,
	})
	return fmt.Sprintf("data: %s\n\n", data)
}

func BuildStreamTextDeltaEvent(itemID, delta string, outputIndex, contentIndex int) string {
	data, _ := json.Marshal(map[string]interface{}{
		"type":          "response.output_text.delta",
		"delta":         delta,
		"item_id":       itemID,
		"output_index":  outputIndex,
		"content_index": contentIndex,
	})
	return fmt.Sprintf("data: %s\n\n", data)
}

func BuildStreamTextDoneEvent(itemID, text string, outputIndex, contentIndex int) string {
	data, _ := json.Marshal(map[string]interface{}{
		"type":          "response.output_text.done",
		"text":          text,
		"item_id":       itemID,
		"output_index":  outputIndex,
		"content_index": contentIndex,
	})
	return fmt.Sprintf("data: %s\n\n", data)
}

func BuildStreamItemAddedEvent(itemID, role string, outputIndex int) string {
	item := map[string]interface{}{
		"id":      itemID,
		"type":    "message",
		"role":    role,
		"content": []interface{}{},
	}
	data, _ := json.Marshal(map[string]interface{}{
		"type":         "response.output_item.added",
		"item_id":      itemID,
		"output_index": outputIndex,
		"item":         item,
	})
	return fmt.Sprintf("data: %s\n\n", data)
}

func BuildStreamFunctionCallItemAddedEvent(itemID, name, callID string, outputIndex int) string {
	item := map[string]interface{}{
		"id":        itemID,
		"type":      "function_call",
		"call_id":   callID,
		"name":      name,
		"arguments": "",
		"status":    "in_progress",
	}
	data, _ := json.Marshal(map[string]interface{}{
		"type":         "response.output_item.added",
		"item_id":      itemID,
		"output_index": outputIndex,
		"item":         item,
	})
	return fmt.Sprintf("data: %s\n\n", data)
}

func BuildStreamFunctionCallArgumentsDeltaEvent(itemID, delta string, outputIndex int) string {
	data, _ := json.Marshal(map[string]interface{}{
		"type":         "response.function_call_arguments.delta",
		"delta":        delta,
		"item_id":      itemID,
		"output_index": outputIndex,
	})
	return fmt.Sprintf("data: %s\n\n", data)
}

func BuildStreamFunctionCallArgumentsDoneEvent(itemID, arguments string, outputIndex int) string {
	data, _ := json.Marshal(map[string]interface{}{
		"type":         "response.function_call_arguments.done",
		"arguments":    arguments,
		"item_id":      itemID,
		"output_index": outputIndex,
	})
	return fmt.Sprintf("data: %s\n\n", data)
}

func BuildStreamOutputItemDoneEvent(itemID string, outputIndex int, item map[string]interface{}) string {
	data, _ := json.Marshal(map[string]interface{}{
		"type":         "response.output_item.done",
		"item_id":      itemID,
		"output_index": outputIndex,
		"item":         item,
	})
	return fmt.Sprintf("data: %s\n\n", data)
}

func BuildStreamCompletedEvent(respID, model string, createdAt int64, status string, output []map[string]interface{}, promptTokens, completionTokens int) string {
	resp := map[string]interface{}{
		"id":         respID,
		"object":     "response",
		"created_at": createdAt,
		"model":      model,
		"status":     status,
		"output":     output,
		"usage": map[string]interface{}{
			"input_tokens":  promptTokens,
			"output_tokens": completionTokens,
			"total_tokens":  promptTokens + completionTokens,
			"output_tokens_details": map[string]interface{}{
				"reasoning_tokens": 0,
			},
		},
	}
	data, _ := json.Marshal(map[string]interface{}{
		"type":     "response.completed",
		"response": resp,
	})
	return fmt.Sprintf("data: %s\n\n", data)
}

type StreamToolCall struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

type StreamState struct {
	RespID           string
	MsgID            string
	Model            string
	CreatedAt        int64
	AccumulatedText  string
	HasContent       bool
	ItemAdded        bool
	TextOutputIndex  int
	PromptTokens     int
	CompletionTokens int
	ToolCalls        []StreamToolCall
	nextOutputIndex  int
}

func NewStreamState(model string, createdAt int64) *StreamState {
	return &StreamState{
		RespID:    responsesIDPrefix + uuid.New().String(),
		Model:     model,
		CreatedAt: createdAt,
	}
}

func (s *StreamState) NextOutputIndex() int {
	idx := s.nextOutputIndex
	s.nextOutputIndex++
	return idx
}

type toolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

func ParseChatStreamLine(line string) (deltaText string, finishReason string, isDone bool, u *usage, toolCalls []toolCallDelta) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", "", false, nil, nil
	}
	if trimmed == "data: [DONE]" {
		return "", "", true, nil, nil
	}

	if !strings.HasPrefix(trimmed, "data: ") {
		return "", "", false, nil, nil
	}

	dataStr := strings.TrimPrefix(trimmed, "data: ")
	var chunk struct {
		Choices []struct {
			Delta struct {
				Content   string          `json:"content"`
				ToolCalls json.RawMessage `json:"tool_calls"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
		Usage *usage `json:"usage,omitempty"`
	}

	if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
		return "", "", false, nil, nil
	}

	u = chunk.Usage

	if len(chunk.Choices) > 0 {
		deltaText = chunk.Choices[0].Delta.Content
		if chunk.Choices[0].FinishReason != nil {
			finishReason = *chunk.Choices[0].FinishReason
		}

		if chunk.Choices[0].Delta.ToolCalls != nil {
			json.Unmarshal(chunk.Choices[0].Delta.ToolCalls, &toolCalls)
		}
	}

	return deltaText, finishReason, false, u, toolCalls
}
