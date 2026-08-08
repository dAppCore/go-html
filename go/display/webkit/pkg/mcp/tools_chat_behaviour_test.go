// pkg/mcp/tools_chat_behaviour_test.go
package mcp

import (
	"context"

	core "dappco.re/go"
)

// chat_send dispatches gui.chat.send and returns the message id.
//
//	c.Action("gui.chat.send", ...) // returns "msg-1"
//	sub.CallTool(ctx, "chat_send", map[string]any{"content": "hi"})
func TestToolsChatBehaviour_Send_Good(t *core.T) {
	c := core.New(core.WithServiceLock())
	okAction(c, "gui.chat.send", "msg-1")
	sub := newToolSubsystem(t, c)

	out, err := sub.CallTool(context.Background(), "chat_send", map[string]any{
		"conversation_id": "conv-1", "content": "hi", "model": "lemma",
	})
	core.RequireNoError(t, err)
	core.AssertContains(t, out, "msg-1")
}

// chat_send surfaces an action failure.
func TestToolsChatBehaviour_Send_Bad(t *core.T) {
	c := core.New(core.WithServiceLock())
	failAction(c, "gui.chat.send", core.NewError("no model"))
	sub := newToolSubsystem(t, c)

	_, err := sub.CallTool(context.Background(), "chat_send", map[string]any{"content": "hi"})
	core.AssertError(t, err)
	core.AssertContains(t, err.Error(), "no model")
}

// chat_history / chat_models / chat_select_model / conversations_{list,load}
// decode their JSON results via decodeChatValue.
func TestToolsChatBehaviour_Reads_Good(t *core.T) {
	c := core.New(core.WithServiceLock())
	okAction(c, "gui.chat.history", []map[string]any{{"id": "m1", "role": "user", "content": "hi"}})
	okAction(c, "gui.chat.models", []map[string]any{{"name": "lemma", "status": "ready"}})
	okAction(c, "gui.chat.select_model", map[string]any{"default_model": "lemma"})
	okAction(c, "gui.chat.conversations.list", []map[string]any{{"id": "conv-1", "title": "Demo"}})
	okAction(c, "gui.chat.conversations.load", map[string]any{"id": "conv-1", "title": "Demo"})
	sub := newToolSubsystem(t, c)

	out, err := sub.CallTool(context.Background(), "chat_history", map[string]any{"conversation_id": "conv-1"})
	core.RequireNoError(t, err)
	core.AssertContains(t, out, "m1")

	out, err = sub.CallTool(context.Background(), "chat_models", map[string]any{})
	core.RequireNoError(t, err)
	core.AssertContains(t, out, "lemma")

	out, err = sub.CallTool(context.Background(), "chat_select_model", map[string]any{"name": "lemma"})
	core.RequireNoError(t, err)
	core.AssertContains(t, out, "lemma")

	out, err = sub.CallTool(context.Background(), "chat_conversations_list", map[string]any{})
	core.RequireNoError(t, err)
	core.AssertContains(t, out, "conv-1")

	out, err = sub.CallTool(context.Background(), "chat_conversations_load", map[string]any{"conversation_id": "conv-1"})
	core.RequireNoError(t, err)
	core.AssertContains(t, out, "Demo")
}

// chat_conversations_delete returns the success bool.
func TestToolsChatBehaviour_Delete_Good(t *core.T) {
	c := core.New(core.WithServiceLock())
	okAction(c, "gui.chat.conversations.delete", true)
	sub := newToolSubsystem(t, c)

	out, err := sub.CallTool(context.Background(), "chat_conversations_delete", map[string]any{"conversation_id": "conv-1"})
	core.RequireNoError(t, err)
	core.AssertContains(t, out, "success")
}

// chat_thinking_start / stop decode a ChatThinkingState.
func TestToolsChatBehaviour_Thinking_Good(t *core.T) {
	c := core.New(core.WithServiceLock())
	okAction(c, "gui.chat.thinking.start", map[string]any{"active": true, "content": "..."})
	okAction(c, "gui.chat.thinking.stop", map[string]any{"active": false, "duration_ms": 1200})
	sub := newToolSubsystem(t, c)

	out, err := sub.CallTool(context.Background(), "chat_thinking_start", map[string]any{"conversation_id": "conv-1"})
	core.RequireNoError(t, err)
	core.AssertContains(t, out, "state")

	out, err = sub.CallTool(context.Background(), "chat_thinking_stop", map[string]any{"conversation_id": "conv-1"})
	core.RequireNoError(t, err)
	core.AssertContains(t, out, "state")
}

// chat_history surfaces an action failure.
func TestToolsChatBehaviour_History_Bad(t *core.T) {
	c := core.New(core.WithServiceLock())
	failAction(c, "gui.chat.history", core.NewError("no conversation"))
	sub := newToolSubsystem(t, c)

	_, err := sub.CallTool(context.Background(), "chat_history", map[string]any{"conversation_id": "x"})
	core.AssertError(t, err)
	core.AssertContains(t, err.Error(), "no conversation")
}
