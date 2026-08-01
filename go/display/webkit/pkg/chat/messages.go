package chat

import "time"

type QueryHistory struct {
	ConversationID string `json:"conversation_id,omitempty"`
	ID             string `json:"id,omitempty"`
}

type QueryModels struct{}

type QuerySettings struct{}

type QuerySettingsDefaults struct{}

type QueryConversationList struct{}

type QueryConversationGet struct {
	ConversationID string `json:"conversation_id,omitempty"`
	ID             string `json:"id,omitempty"`
}

type QueryConversationSearch struct {
	Query string `json:"q"`
}

// Message is the persisted chat transcript entry used by the MVP IPC surface.
type Message struct {
	ID           string            `json:"id"`
	Role         string            `json:"role"`
	Content      string            `json:"content"`
	CreatedAt    time.Time         `json:"created_at"`
	Model        string            `json:"model,omitempty"`
	Attachments  []ImageAttachment `json:"attachments,omitempty"`
	Thinking     *ThinkingState    `json:"thinking,omitempty"`
	ToolCalls    []ToolCall        `json:"tool_calls,omitempty"`
	ToolResults  []ToolResult      `json:"tool_results,omitempty"`
	ToolCallID   string            `json:"tool_call_id,omitempty"`
	FinishReason string            `json:"finish_reason,omitempty"`
}

// ChatMessage aliases Message to preserve the original public IPC type name.
type ChatMessage = Message

// Model is the transport shape exposed by gui.chat.models.
type Model struct {
	Name           string `json:"name"`
	Size           int64  `json:"size"` // Size mirrors SizeBytes for legacy clients that read the original field.
	Status         string `json:"status"`
	Architecture   string `json:"architecture,omitempty"`
	QuantBits      int    `json:"quant_bits,omitempty"`
	SizeBytes      int64  `json:"size_bytes,omitempty"` // SizeBytes is the exact model size in bytes.
	Loaded         bool   `json:"loaded,omitempty"`
	Backend        string `json:"backend,omitempty"`
	SupportsVision bool   `json:"supports_vision,omitempty"`
}

// ModelEntry aliases Model for backwards-compatible model list APIs.
type ModelEntry = Model

type ChatSettings struct {
	Temperature   float32 `json:"temperature"`
	TopP          float32 `json:"top_p"`
	TopK          int     `json:"top_k"`
	MaxTokens     int     `json:"max_tokens"`
	ContextWindow int     `json:"context_window"`
	SystemPrompt  string  `json:"system_prompt"`
	DefaultModel  string  `json:"default_model"`
}

type Conversation struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Model     string        `json:"model"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	Messages  []Message     `json:"messages"`
	Settings  *ChatSettings `json:"settings,omitempty"`
}

type ConversationSummary struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Model        string    `json:"model"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int       `json:"message_count"`
}

type ThinkingState struct {
	Active     bool      `json:"active"`
	Content    string    `json:"content"`
	StartedAt  time.Time `json:"started_at"`
	EndedAt    time.Time `json:"ended_at"`
	DurationMS int64     `json:"duration_ms,omitempty"`
}

type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
}

type ImageAttachment struct {
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
	Data     string `json:"data"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

func DefaultSettings() ChatSettings {
	return ChatSettings{
		Temperature:   1.0,
		TopP:          0.95,
		TopK:          64,
		MaxTokens:     2048,
		ContextWindow: 8192,
		SystemPrompt:  "You are a helpful assistant.",
	}
}

func (c Conversation) Summary() ConversationSummary {
	return ConversationSummary{
		ID:           c.ID,
		Title:        c.Title,
		Model:        c.Model,
		CreatedAt:    c.CreatedAt,
		UpdatedAt:    c.UpdatedAt,
		MessageCount: len(c.Messages),
	}
}

type ActionConversationCreated struct {
	Conversation Conversation `json:"conversation"`
}

type ActionConversationUpdated struct {
	Conversation Conversation `json:"conversation"`
}

type ActionConversationDeleted struct {
	ConversationID string `json:"conversation_id"`
}

type ActionMessageAdded struct {
	ConversationID string  `json:"conversation_id"`
	Message        Message `json:"message"`
}

type ActionConversationCleared struct {
	ConversationID string `json:"conversation_id"`
}

type ActionStreamStarted struct {
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	StreamID       string `json:"stream_id"`
}

type ActionTokenAppended struct {
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	Content        string `json:"content"`
}

type ActionStreamFinished struct {
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	FinishReason   string `json:"finish_reason,omitempty"`
}

type ActionThinkingStarted struct {
	ConversationID string    `json:"conversation_id"`
	MessageID      string    `json:"message_id"`
	StartedAt      time.Time `json:"started_at"`
}

type ActionThinkingAppended struct {
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	Content        string `json:"content"`
}

type ActionThinkingEnded struct {
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	DurationMS     int64  `json:"duration_ms"`
}

type ActionToolCallStarted struct {
	ConversationID string   `json:"conversation_id"`
	MessageID      string   `json:"message_id"`
	Call           ToolCall `json:"call"`
}

type ActionToolResultReady struct {
	ConversationID string     `json:"conversation_id"`
	MessageID      string     `json:"message_id"`
	Result         ToolResult `json:"result"`
}

type ActionImageQueued struct {
	ConversationID string          `json:"conversation_id"`
	Attachment     ImageAttachment `json:"attachment"`
}
