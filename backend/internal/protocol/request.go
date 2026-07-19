package protocol

import "strings"

type Protocol string

const (
	ProtocolResponses         Protocol = "responses"
	ProtocolChatCompletions   Protocol = "chat_completions"
	ProtocolAnthropicMessages Protocol = "anthropic_messages"
)

type TurnRequest struct {
	Protocol         Protocol
	ConversationID   string
	Model            string
	Stream           bool
	SystemContent    string
	DeveloperContent string
	AssistantContent string
	UserContent      string
	LastUserContent  string
	InputParts       []InputPart
	ToolSchemas      []ToolSchema
	BuiltinTools     []BuiltinTool
	ToolChoice       ToolChoice
	ResponseFormat   ResponseFormat
	Options          TurnOptions
	RawBody          map[string]any
}

type OutputSegment struct {
	Mode                string `json:"mode"`
	Text                string `json:"text"`
	ReasoningStreamMode string `json:"reasoning_stream_mode,omitempty"`
}

type TurnResult struct {
	ResponseID          string
	OutputText          string
	Mode                string
	ReasoningStreamMode string
	ToolName            string
	ToolCallID          string
	ToolOutput          string
	OutputSegments      []OutputSegment
	Usage               Usage
	FinishReason        string
	StopSequence        string
}

type ConversationMeta struct {
	Protocol   Protocol
	Model      string
	ResponseID string
}

type InputPart struct {
	Type       string
	Text       string
	MediaType  string
	URL        string
	ToolCallID string
}

type ToolChoice struct {
	Type string
	Name string
}

type ResponseFormat struct {
	Type   string
	Name   string
	Schema map[string]any
}

type NormalizedToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Type        string         `json:"type"`
}

type ToolSchema struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Raw         map[string]any `json:"raw,omitempty"`
}

type BuiltinTool struct {
	Kind  string         `json:"kind"`
	Type  string         `json:"type"`
	Label string         `json:"label,omitempty"`
	Raw   map[string]any `json:"raw,omitempty"`
}

type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

func ParseRequest(protocolValue string, body map[string]any) TurnRequest {
	proto := ParseProtocol(protocolValue)
	inputParts := extractRequestInputParts(proto, body)
	return TurnRequest{
		Protocol:         proto,
		ConversationID:   stringValue(body["conversation_id"], ""),
		Model:            stringValue(body["model"], "chatapi-lab"),
		Stream:           boolValue(body["stream"]),
		SystemContent:    extractRequestRoleContent(proto, body, "system"),
		DeveloperContent: extractRequestRoleContent(proto, body, "developer"),
		AssistantContent: extractRequestRoleContent(proto, body, "assistant"),
		UserContent:      joinInputPartText(inputParts),
		LastUserContent:  extractLastUserContent(proto, body),
		InputParts:       inputParts,
		ToolSchemas:      extractToolSchemas(body),
		BuiltinTools:     extractBuiltinTools(proto, body),
		ToolChoice:       extractToolChoice(body),
		ResponseFormat:   extractResponseFormat(body),
		Options:          extractTurnOptions(proto, body),
		RawBody:          cloneAnyMap(body),
	}
}

func ParseProtocol(value string) Protocol {
	switch strings.TrimSpace(value) {
	case string(ProtocolChatCompletions):
		return ProtocolChatCompletions
	case string(ProtocolAnthropicMessages):
		return ProtocolAnthropicMessages
	default:
		return ProtocolResponses
	}
}

func (p Protocol) String() string {
	return string(p)
}

func (p Protocol) IsAnthropicMessages() bool {
	return p == ProtocolAnthropicMessages
}

func extractRequestInputParts(proto Protocol, body map[string]any) []InputPart {
	switch proto {
	case ProtocolChatCompletions:
		return extractChatCompletionsInputParts(body)
	case ProtocolAnthropicMessages:
		return extractAnthropicInputParts(body)
	default:
		return extractResponsesInputParts(body)
	}
}

func extractRequestRoleContent(proto Protocol, body map[string]any, role string) string {
	switch proto {
	case ProtocolChatCompletions:
		return extractChatCompletionsRoleContent(body, role)
	case ProtocolAnthropicMessages:
		return extractAnthropicRoleContent(body, role)
	default:
		return extractResponsesRoleContent(body, role)
	}
}
