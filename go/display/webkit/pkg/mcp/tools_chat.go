package mcp

import (
	"context"
	"time"

	core "dappco.re/go"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ChatMessage struct {
	ID           string                `json:"id"`
	Role         string                `json:"role"`
	Content      string                `json:"content"`
	CreatedAt    time.Time             `json:"created_at"`
	Model        string                `json:"model,omitempty"`
	Attachments  []ChatImageAttachment `json:"attachments,omitempty"`
	Thinking     *ChatThinkingState    `json:"thinking,omitempty"`
	ToolCalls    []ChatToolCall        `json:"tool_calls,omitempty"`
	ToolResults  []ChatToolResult      `json:"tool_results,omitempty"`
	ToolCallID   string                `json:"tool_call_id,omitempty"`
	FinishReason string                `json:"finish_reason,omitempty"`
}

type ChatModel struct {
	Name           string `json:"name"`
	Size           int64  `json:"size"`
	Status         string `json:"status"`
	Architecture   string `json:"architecture,omitempty"`
	QuantBits      int    `json:"quant_bits,omitempty"`
	SizeBytes      int64  `json:"size_bytes,omitempty"`
	Loaded         bool   `json:"loaded,omitempty"`
	Backend        string `json:"backend,omitempty"`
	SupportsVision bool   `json:"supports_vision,omitempty"`
}

type ChatSettings struct {
	Temperature   float32 `json:"temperature"`
	TopP          float32 `json:"top_p"`
	TopK          int     `json:"top_k"`
	MaxTokens     int     `json:"max_tokens"`
	ContextWindow int     `json:"context_window"`
	SystemPrompt  string  `json:"system_prompt"`
	DefaultModel  string  `json:"default_model"`
}

type ChatConversation struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Model     string        `json:"model"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	Messages  []ChatMessage `json:"messages"`
	Settings  *ChatSettings `json:"settings,omitempty"`
}

type ChatThinkingState struct {
	Active     bool      `json:"active"`
	Content    string    `json:"content"`
	StartedAt  time.Time `json:"started_at"`
	EndedAt    time.Time `json:"ended_at"`
	DurationMS int64     `json:"duration_ms,omitempty"`
}

type ChatToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type ChatToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
}

type ChatImageAttachment struct {
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
	Data     string `json:"data"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

type ChatSendInput struct {
	ConversationID string `json:"conversation_id,omitempty"`
	Content        string `json:"content"`
	Model          string `json:"model,omitempty"`
}

type ChatSendOutput struct {
	MessageID string `json:"message_id"`
}

func (s *Subsystem) chatSend(_ context.Context, _ *mcp.CallToolRequest, input ChatSendInput) (*mcp.CallToolResult, ChatSendOutput, resultFailure) {
	result := s.core.Action("gui.chat.send").Run(context.Background(), core.NewOptions(
		core.Option{Key: "conversation_id", Value: input.ConversationID},
		core.Option{Key: "content", Value: input.Content},
		core.Option{Key: "model", Value: input.Model},
	))
	if !result.OK {
		if err, ok := result.Value.(error); ok {
			return nil, ChatSendOutput{}, err
		}
		return nil, ChatSendOutput{}, core.E("mcp.chatSend", "chat send failed", nil)
	}
	messageID, ok := result.Value.(string)
	if !ok {
		return nil, ChatSendOutput{}, core.E("mcp.chatSend", "unexpected result type", nil)
	}
	return nil, ChatSendOutput{MessageID: messageID}, nil
}

type ChatHistoryInput struct {
	ConversationID string `json:"conversation_id,omitempty"`
	ID             string `json:"id,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}

type ChatHistoryOutput struct {
	Messages []ChatMessage `json:"messages"`
}

func (s *Subsystem) chatHistory(_ context.Context, _ *mcp.CallToolRequest, input ChatHistoryInput) (*mcp.CallToolResult, ChatHistoryOutput, resultFailure) {
	result := s.core.Action("gui.chat.history").Run(context.Background(), core.NewOptions(
		core.Option{Key: "conversation_id", Value: input.ConversationID},
		core.Option{Key: "id", Value: input.ID},
		core.Option{Key: "limit", Value: input.Limit},
	))
	if !result.OK {
		if err, ok := result.Value.(error); ok {
			return nil, ChatHistoryOutput{}, err
		}
		return nil, ChatHistoryOutput{}, core.E("mcp.chatHistory", "chat history failed", nil)
	}
	messages, err := decodeChatValue[[]ChatMessage](result.Value)
	if err != nil {
		return nil, ChatHistoryOutput{}, err
	}
	return nil, ChatHistoryOutput{Messages: messages}, nil
}

type ChatModelsInput struct{}

type ChatModelsOutput struct {
	Models []ChatModel `json:"models"`
}

func (s *Subsystem) chatModels(_ context.Context, _ *mcp.CallToolRequest, _ ChatModelsInput) (*mcp.CallToolResult, ChatModelsOutput, resultFailure) {
	result := s.core.Action("gui.chat.models").Run(context.Background(), core.NewOptions())
	if !result.OK {
		if err, ok := result.Value.(error); ok {
			return nil, ChatModelsOutput{}, err
		}
		return nil, ChatModelsOutput{}, core.E("mcp.chatModels", "chat models failed", nil)
	}
	models, err := decodeChatValue[[]ChatModel](result.Value)
	if err != nil {
		return nil, ChatModelsOutput{}, err
	}
	return nil, ChatModelsOutput{Models: models}, nil
}

type ChatSelectModelInput struct {
	Name           string `json:"name,omitempty"`
	Model          string `json:"model,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	ID             string `json:"id,omitempty"`
}

type ChatSelectModelOutput struct {
	Settings ChatSettings `json:"settings"`
}

func (s *Subsystem) chatSelectModel(_ context.Context, _ *mcp.CallToolRequest, input ChatSelectModelInput) (*mcp.CallToolResult, ChatSelectModelOutput, resultFailure) {
	result := s.core.Action("gui.chat.select_model").Run(context.Background(), core.NewOptions(
		core.Option{Key: "name", Value: input.Name},
		core.Option{Key: "model", Value: input.Model},
		core.Option{Key: "conversation_id", Value: input.ConversationID},
		core.Option{Key: "id", Value: input.ID},
	))
	if !result.OK {
		if err, ok := result.Value.(error); ok {
			return nil, ChatSelectModelOutput{}, err
		}
		return nil, ChatSelectModelOutput{}, core.E("mcp.chatSelectModel", "select model failed", nil)
	}
	settings, err := decodeChatValue[ChatSettings](result.Value)
	if err != nil {
		return nil, ChatSelectModelOutput{}, err
	}
	return nil, ChatSelectModelOutput{Settings: settings}, nil
}

type ChatConversationsListInput struct{}

type ChatConversationsListOutput struct {
	Conversations []ChatConversation `json:"conversations"`
}

func (s *Subsystem) chatConversationsList(_ context.Context, _ *mcp.CallToolRequest, _ ChatConversationsListInput) (*mcp.CallToolResult, ChatConversationsListOutput, resultFailure) {
	result := s.core.Action("gui.chat.conversations.list").Run(context.Background(), core.NewOptions())
	if !result.OK {
		if err, ok := result.Value.(error); ok {
			return nil, ChatConversationsListOutput{}, err
		}
		return nil, ChatConversationsListOutput{}, core.E("mcp.chatConversationsList", "list conversations failed", nil)
	}
	conversations, err := decodeChatValue[[]ChatConversation](result.Value)
	if err != nil {
		return nil, ChatConversationsListOutput{}, err
	}
	return nil, ChatConversationsListOutput{Conversations: conversations}, nil
}

type ChatConversationsLoadInput struct {
	ConversationID string `json:"conversation_id,omitempty"`
	ID             string `json:"id,omitempty"`
}

type ChatConversationsLoadOutput struct {
	Conversation ChatConversation `json:"conversation"`
}

func (s *Subsystem) chatConversationsLoad(_ context.Context, _ *mcp.CallToolRequest, input ChatConversationsLoadInput) (*mcp.CallToolResult, ChatConversationsLoadOutput, resultFailure) {
	result := s.core.Action("gui.chat.conversations.load").Run(context.Background(), core.NewOptions(
		core.Option{Key: "conversation_id", Value: input.ConversationID},
		core.Option{Key: "id", Value: input.ID},
	))
	if !result.OK {
		if err, ok := result.Value.(error); ok {
			return nil, ChatConversationsLoadOutput{}, err
		}
		return nil, ChatConversationsLoadOutput{}, core.E("mcp.chatConversationsLoad", "load conversation failed", nil)
	}
	conversation, err := decodeChatValue[ChatConversation](result.Value)
	if err != nil {
		return nil, ChatConversationsLoadOutput{}, err
	}
	return nil, ChatConversationsLoadOutput{Conversation: conversation}, nil
}

type ChatConversationsDeleteInput struct {
	ConversationID string `json:"conversation_id,omitempty"`
	ID             string `json:"id,omitempty"`
}

type ChatConversationsDeleteOutput struct {
	Success bool `json:"success"`
}

func (s *Subsystem) chatConversationsDelete(_ context.Context, _ *mcp.CallToolRequest, input ChatConversationsDeleteInput) (*mcp.CallToolResult, ChatConversationsDeleteOutput, resultFailure) {
	result := s.core.Action("gui.chat.conversations.delete").Run(context.Background(), core.NewOptions(
		core.Option{Key: "conversation_id", Value: input.ConversationID},
		core.Option{Key: "id", Value: input.ID},
	))
	if !result.OK {
		if err, ok := result.Value.(error); ok {
			return nil, ChatConversationsDeleteOutput{}, err
		}
		return nil, ChatConversationsDeleteOutput{}, core.E("mcp.chatConversationsDelete", "delete conversation failed", nil)
	}
	success, ok := result.Value.(bool)
	if !ok {
		return nil, ChatConversationsDeleteOutput{}, core.E("mcp.chatConversationsDelete", "unexpected result type", nil)
	}
	return nil, ChatConversationsDeleteOutput{Success: success}, nil
}

type ChatThinkingStartInput struct {
	ConversationID string    `json:"conversation_id"`
	MessageID      string    `json:"message_id,omitempty"`
	StartedAt      time.Time `json:"started_at"`
}

type ChatThinkingStartOutput struct {
	State ChatThinkingState `json:"state"`
}

func (s *Subsystem) chatThinkingStart(_ context.Context, _ *mcp.CallToolRequest, input ChatThinkingStartInput) (*mcp.CallToolResult, ChatThinkingStartOutput, resultFailure) {
	result := s.core.Action("gui.chat.thinking.start").Run(context.Background(), core.NewOptions(
		core.Option{Key: "conversation_id", Value: input.ConversationID},
		core.Option{Key: "message_id", Value: input.MessageID},
		core.Option{Key: "started_at", Value: input.StartedAt},
	))
	if !result.OK {
		if err, ok := result.Value.(error); ok {
			return nil, ChatThinkingStartOutput{}, err
		}
		return nil, ChatThinkingStartOutput{}, core.E("mcp.chatThinkingStart", "thinking start failed", nil)
	}
	state, err := decodeChatValue[ChatThinkingState](result.Value)
	if err != nil {
		return nil, ChatThinkingStartOutput{}, err
	}
	return nil, ChatThinkingStartOutput{State: state}, nil
}

type ChatThinkingStopInput struct {
	ConversationID string    `json:"conversation_id"`
	MessageID      string    `json:"message_id,omitempty"`
	StartedAt      time.Time `json:"started_at"`
	DurationMS     int64     `json:"duration_ms,omitempty"`
}

type ChatThinkingStopOutput struct {
	State ChatThinkingState `json:"state"`
}

func (s *Subsystem) chatThinkingStop(_ context.Context, _ *mcp.CallToolRequest, input ChatThinkingStopInput) (*mcp.CallToolResult, ChatThinkingStopOutput, resultFailure) {
	result := s.core.Action("gui.chat.thinking.stop").Run(context.Background(), core.NewOptions(
		core.Option{Key: "conversation_id", Value: input.ConversationID},
		core.Option{Key: "message_id", Value: input.MessageID},
		core.Option{Key: "started_at", Value: input.StartedAt},
		core.Option{Key: "duration_ms", Value: input.DurationMS},
	))
	if !result.OK {
		if err, ok := result.Value.(error); ok {
			return nil, ChatThinkingStopOutput{}, err
		}
		return nil, ChatThinkingStopOutput{}, core.E("mcp.chatThinkingStop", "thinking stop failed", nil)
	}
	state, err := decodeChatValue[ChatThinkingState](result.Value)
	if err != nil {
		return nil, ChatThinkingStopOutput{}, err
	}
	return nil, ChatThinkingStopOutput{State: state}, nil
}

func decodeChatValue[T any](value any) (T, resultFailure) {
	var output T
	result := core.JSONUnmarshalString(core.JSONMarshalString(value), &output)
	if result.OK {
		return output, nil
	}
	if err, ok := result.Value.(error); ok {
		return output, err
	}
	return output, core.E("mcp.decodeChatValue", "failed to decode chat value", nil)
}

func (s *Subsystem) registerChatTools(server *mcp.Server) {
	addTool(s, server, &mcp.Tool{
		Name:        "chat_send",
		Description: `Send a chat message and return the streamed assistant message id. Example: {"conversation_id":"conv-1","content":"Hello","model":"lemma"}`,
	}, s.chatSend)
	addTool(s, server, &mcp.Tool{
		Name:        "chat_history",
		Description: `Read chat message history for a conversation. Example: {"conversation_id":"conv-1","limit":20}`,
	}, s.chatHistory)
	addTool(s, server, &mcp.Tool{
		Name:        "chat_models",
		Description: "List available chat models with size and status metadata",
	}, s.chatModels)
	addTool(s, server, &mcp.Tool{
		Name:        "chat_select_model",
		Description: `Set the active chat model. Example: {"name":"lemma","conversation_id":"conv-1"}`,
	}, s.chatSelectModel)
	addTool(s, server, &mcp.Tool{
		Name:        "chat_conversations_list",
		Description: "List stored chat conversations",
	}, s.chatConversationsList)
	addTool(s, server, &mcp.Tool{
		Name:        "chat_conversations_load",
		Description: `Load a stored chat conversation by id. Example: {"conversation_id":"conv-1"}`,
	}, s.chatConversationsLoad)
	addTool(s, server, &mcp.Tool{
		Name:        "chat_conversations_delete",
		Description: `Delete a stored chat conversation. Example: {"conversation_id":"conv-1"}`,
	}, s.chatConversationsDelete)
	addTool(s, server, &mcp.Tool{
		Name:        "chat_thinking_start",
		Description: `Mark a conversation as thinking. Example: {"conversation_id":"conv-1"}`,
	}, s.chatThinkingStart)
	addTool(s, server, &mcp.Tool{
		Name:        "chat_thinking_stop",
		Description: `Clear the thinking state for a conversation. Example: {"conversation_id":"conv-1"}`,
	}, s.chatThinkingStop)
}
