package main

import (
	"encoding/json"
	"strings"
)

type ParsedRequest struct {
	Model     string
	MaxTokens int
	System    string
	Messages  []ParsedMessage
	Raw       map[string]interface{}
}

type ParsedMessage struct {
	Role        string
	TextContent string
	Content     []ContentBlock
	Raw         map[string]interface{}
}

type ParsedResponse struct {
	Content    []ContentBlock
	Usage      UsageInfo
	StopReason string
	Raw        map[string]interface{}
}

type ContentBlock struct {
	Type      string
	Text      string
	Thinking  string
	ToolID    string
	ToolName  string
	ToolInput map[string]interface{}
	IsError   bool
	Raw       map[string]interface{}
}

type UsageInfo struct {
	InputTokens              int
	OutputTokens             int
	CacheReadInputTokens     int
	CacheCreationInputTokens int
}

func ParseRequestBody(body string, host string) ParsedRequest {
	var raw map[string]interface{}
	if json.Unmarshal([]byte(body), &raw) != nil {
		return ParsedRequest{Raw: raw}
	}

	parsed := ParsedRequest{Raw: raw}

	if model, ok := raw["model"].(string); ok {
		parsed.Model = model
	}
	if maxTokens, ok := raw["max_tokens"].(float64); ok {
		parsed.MaxTokens = int(maxTokens)
	}
	// Handle system as string
	if system, ok := raw["system"].(string); ok {
		parsed.System = system
	}
	// Handle system as array of content blocks
	if systemArr, ok := raw["system"].([]interface{}); ok {
		var systemParts []string
		for _, item := range systemArr {
			if block, ok := item.(map[string]interface{}); ok {
				if text, ok := block["text"].(string); ok {
					systemParts = append(systemParts, text)
				}
			}
		}
		parsed.System = strings.Join(systemParts, "\n\n")
	}

	if messages, ok := raw["messages"].([]interface{}); ok {
		for _, m := range messages {
			if msg, ok := m.(map[string]interface{}); ok {
				pm := ParsedMessage{Raw: msg}

				if role, ok := msg["role"].(string); ok {
					pm.Role = role
				}

				// Handle simple string content
				if content, ok := msg["content"].(string); ok {
					pm.TextContent = content
				}

				// Handle array content (tool results, etc)
				if content, ok := msg["content"].([]interface{}); ok {
					for _, c := range content {
						if block, ok := c.(map[string]interface{}); ok {
							pm.Content = append(pm.Content, parseContentBlock(block))
						}
					}
					// Set TextContent from first text block for convenience
					for _, cb := range pm.Content {
						if cb.Type == "text" && pm.TextContent == "" {
							pm.TextContent = cb.Text
						}
					}
				}

				parsed.Messages = append(parsed.Messages, pm)
			}
		}
	}

	return parsed
}

func ParseResponseBody(body string, host string) ParsedResponse {
	var raw map[string]interface{}
	if json.Unmarshal([]byte(body), &raw) != nil {
		return ParsedResponse{Raw: raw}
	}

	parsed := ParsedResponse{Raw: raw}

	if content, ok := raw["content"].([]interface{}); ok {
		for _, c := range content {
			if block, ok := c.(map[string]interface{}); ok {
				parsed.Content = append(parsed.Content, parseContentBlock(block))
			}
		}
	}

	if usage, ok := raw["usage"].(map[string]interface{}); ok {
		if in, ok := usage["input_tokens"].(float64); ok {
			parsed.Usage.InputTokens = int(in)
		}
		if out, ok := usage["output_tokens"].(float64); ok {
			parsed.Usage.OutputTokens = int(out)
		}
		if cacheRead, ok := usage["cache_read_input_tokens"].(float64); ok {
			parsed.Usage.CacheReadInputTokens = int(cacheRead)
		}
		if cacheCreate, ok := usage["cache_creation_input_tokens"].(float64); ok {
			parsed.Usage.CacheCreationInputTokens = int(cacheCreate)
		}
	}

	if stop, ok := raw["stop_reason"].(string); ok {
		parsed.StopReason = stop
	}

	return parsed
}

// ParseStreamingResponse reconstructs a ParsedResponse from SSE chunks
func ParseStreamingResponse(chunks []StreamChunk) ParsedResponse {
	parsed := ParsedResponse{}

	// Track content blocks being built
	var currentBlocks []ContentBlock
	blockInputBuilders := make(map[int]string) // For building tool input JSON
	blockTextBuilders := make(map[int]string)  // For building text content
	blockThinkingBuilders := make(map[int]string) // For building thinking content

	for _, chunk := range chunks {
		// Parse SSE format: "event: <type>\n" followed by "data: <json>\n"
		raw := chunk.Raw
		if strings.HasPrefix(raw, "data: ") {
			dataStr := strings.TrimPrefix(raw, "data: ")
			dataStr = strings.TrimSpace(dataStr)

			var data map[string]interface{}
			if json.Unmarshal([]byte(dataStr), &data) != nil {
				continue
			}

			eventType, _ := data["type"].(string)

			switch eventType {
			case "message_start":
				// Extract usage from message_start
				if msg, ok := data["message"].(map[string]interface{}); ok {
					if usage, ok := msg["usage"].(map[string]interface{}); ok {
						if in, ok := usage["input_tokens"].(float64); ok {
							parsed.Usage.InputTokens = int(in)
						}
						if cacheRead, ok := usage["cache_read_input_tokens"].(float64); ok {
							parsed.Usage.CacheReadInputTokens = int(cacheRead)
						}
						if cacheCreate, ok := usage["cache_creation_input_tokens"].(float64); ok {
							parsed.Usage.CacheCreationInputTokens = int(cacheCreate)
						}
					}
				}

			case "content_block_start":
				idx := 0
				if i, ok := data["index"].(float64); ok {
					idx = int(i)
				}
				// Ensure we have enough blocks
				for len(currentBlocks) <= idx {
					currentBlocks = append(currentBlocks, ContentBlock{})
				}
				if block, ok := data["content_block"].(map[string]interface{}); ok {
					if t, ok := block["type"].(string); ok {
						currentBlocks[idx].Type = t
					}
					if id, ok := block["id"].(string); ok {
						currentBlocks[idx].ToolID = id
					}
					if name, ok := block["name"].(string); ok {
						currentBlocks[idx].ToolName = name
					}
				}

			case "content_block_delta":
				idx := 0
				if i, ok := data["index"].(float64); ok {
					idx = int(i)
				}
				if delta, ok := data["delta"].(map[string]interface{}); ok {
					deltaType, _ := delta["type"].(string)
					switch deltaType {
					case "text_delta":
						if text, ok := delta["text"].(string); ok {
							blockTextBuilders[idx] += text
						}
					case "thinking_delta":
						if thinking, ok := delta["thinking"].(string); ok {
							blockThinkingBuilders[idx] += thinking
						}
					case "input_json_delta":
						if partial, ok := delta["partial_json"].(string); ok {
							blockInputBuilders[idx] += partial
						}
					}
				}

			case "content_block_stop":
				idx := 0
				if i, ok := data["index"].(float64); ok {
					idx = int(i)
				}
				// Finalize the content block
				if idx < len(currentBlocks) {
					if text, ok := blockTextBuilders[idx]; ok && text != "" {
						currentBlocks[idx].Text = text
					}
					if thinking, ok := blockThinkingBuilders[idx]; ok && thinking != "" {
						currentBlocks[idx].Thinking = thinking
					}
					if inputJSON, ok := blockInputBuilders[idx]; ok && inputJSON != "" {
						var input map[string]interface{}
						if json.Unmarshal([]byte(inputJSON), &input) == nil {
							currentBlocks[idx].ToolInput = input
						}
					}
				}

			case "message_delta":
				if usage, ok := data["usage"].(map[string]interface{}); ok {
					if out, ok := usage["output_tokens"].(float64); ok {
						parsed.Usage.OutputTokens = int(out)
					}
				}
				if delta, ok := data["delta"].(map[string]interface{}); ok {
					if stop, ok := delta["stop_reason"].(string); ok {
						parsed.StopReason = stop
					}
				}
			}
		}
	}

	parsed.Content = currentBlocks
	return parsed
}

func parseContentBlock(block map[string]interface{}) ContentBlock {
	cb := ContentBlock{Raw: block}

	if t, ok := block["type"].(string); ok {
		cb.Type = t
	}

	switch cb.Type {
	case "text":
		if text, ok := block["text"].(string); ok {
			cb.Text = text
		}
	case "thinking":
		if thinking, ok := block["thinking"].(string); ok {
			cb.Thinking = thinking
		}
	case "tool_use":
		if id, ok := block["id"].(string); ok {
			cb.ToolID = id
		}
		if name, ok := block["name"].(string); ok {
			cb.ToolName = name
		}
		if input, ok := block["input"].(map[string]interface{}); ok {
			cb.ToolInput = input
		}
	case "tool_result":
		if id, ok := block["tool_use_id"].(string); ok {
			cb.ToolID = id
		}
		if content, ok := block["content"].(string); ok {
			cb.Text = content
		}
		if isError, ok := block["is_error"].(bool); ok {
			cb.IsError = isError
		}
	}

	return cb
}
