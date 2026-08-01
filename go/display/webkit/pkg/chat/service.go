package chat

import (
	"context"
	"encoding/base64"
	"image"
	"io"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"

	core "dappco.re/go"
	"dappco.re/go/render/display/webkit/pkg/internal/coreutil"
	guimcp "dappco.re/go/render/display/webkit/pkg/mcp"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

const (
	conversationsGroup = "chat_conversations"
	settingsGroup      = "chat_settings"
	settingsKey        = "global"
	maxToolRounds      = 3
)

type Options struct {
	APIURL       string
	StorePath    string
	HTTPClient   *http.Client
	ModelRoots   []string
	ToolExecutor ToolExecutor
	Now          func() time.Time
}

type contract interface {
	Send(context.Context, sendInput) (string, resultFailure)
	History(string, int) ([]Message, resultFailure)
	Models() []ModelEntry
	SelectModel(selectModelInput) (ChatSettings, resultFailure)
	ListConversations() ([]Conversation, resultFailure)
	LoadConversation(string) (Conversation, resultFailure)
	DeleteConversation(string) resultFailure
	StartThinking(thinkingInput) (ThinkingState, resultFailure)
	StopThinking(thinkingInput) (ThinkingState, resultFailure)
}

var _ contract = (*Service)(nil)

type Service struct {
	*core.ServiceRuntime[Options]
	options            Options
	store              *chatStore
	httpClient         *http.Client
	toolExecutor       ToolExecutor
	toolHandler        ToolCallHandler
	pendingAttachments map[string][]ImageAttachment
	thinkingStates     map[string]ThinkingState
	mu                 sync.Mutex
}

type sendInput struct {
	ConversationID string `json:"conversation_id,omitempty"`
	Content        string `json:"content"`
	Model          string `json:"model,omitempty"`
}

type historyInput struct {
	ID             string `json:"id,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}

type conversationInput struct {
	ID             string `json:"id,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
}

type searchInput struct {
	Query string `json:"q"`
}

type renameInput struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type thinkingInput struct {
	ConversationID string    `json:"conversation_id,omitempty"`
	MessageID      string    `json:"message_id,omitempty"`
	Content        string    `json:"content,omitempty"`
	StartedAt      time.Time `json:"started_at"`
	DurationMS     int64     `json:"duration_ms,omitempty"`
}

type selectModelInput struct {
	Name           string `json:"name,omitempty"`
	Model          string `json:"model"`
	ConversationID string `json:"conversation_id,omitempty"`
	ID             string `json:"id,omitempty"`
}

type attachImageInput struct {
	ConversationID  string `json:"conversation_id,omitempty"`
	ImageAttachment `json:",inline"`
}

type removeImageInput struct {
	ConversationID string `json:"conversation_id,omitempty"`
	Index          int    `json:"index"`
}

type attachImageFileInput struct {
	ConversationID string `json:"conversation_id,omitempty"`
	Path           string `json:"path,omitempty"`
}

type openAIRequest struct {
	Model       string           `json:"model"`
	Messages    []openAIMessage  `json:"messages"`
	Temperature float32          `json:"temperature,omitempty"`
	TopP        float32          `json:"top_p,omitempty"`
	TopK        int              `json:"top_k,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Stream      bool             `json:"stream"`
	Tools       []openAIToolSpec `json:"tools,omitempty"`
	ToolChoice  string           `json:"tool_choice,omitempty"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAIToolSpec struct {
	Type     string             `json:"type"`
	Function openAIFunctionSpec `json:"function"`
}

type openAIFunctionSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type configuredModels struct {
	Default      string `yaml:"default"`
	DefaultModel string `yaml:"default_model"`
	Models       []struct {
		Name           string `yaml:"name"`
		Path           string `yaml:"path,omitempty"`
		Architecture   string `yaml:"architecture"`
		Backend        string `yaml:"backend"`
		SupportsVision *bool  `yaml:"supports_vision"`
	} `yaml:"models"`
}

type modelConfig struct {
	ModelType    string `json:"model_type"`
	Quantization struct {
		Bits int `json:"bits"`
	} `json:"quantization"`
}

var supportedImageMimeTypes = map[string]struct{}{
	"image/png":  {},
	"image/jpeg": {},
	"image/webp": {},
	"image/gif":  {},
}

func Register(optionFns ...func(*Options)) func(*core.Core) core.Result {
	options := Options{
		APIURL:     "http://localhost:8090",
		StorePath:  core.PathJoin(core.Env("DIR_HOME"), ".core", "gui", "chat.db"),
		HTTPClient: &http.Client{Timeout: 5 * time.Minute},
		ModelRoots: defaultModelRoots(),
		Now:        time.Now,
	}
	for _, fn := range optionFns {
		fn(&options)
	}
	return func(c *core.Core) core.Result {
		svc := &Service{
			ServiceRuntime:     core.NewServiceRuntime[Options](c, options),
			options:            options,
			httpClient:         options.HTTPClient,
			pendingAttachments: make(map[string][]ImageAttachment),
			thinkingStates:     make(map[string]ThinkingState),
		}
		return core.Result{Value: svc, OK: true}
	}
}

func (s *Service) OnStartup(_ context.Context) core.Result {
	keyValueStore, err := newChatStore(s.options.StorePath)
	if err != nil {
		return core.Result{Value: err, OK: false}
	}
	s.store = keyValueStore

	s.toolExecutor = s.options.ToolExecutor
	if s.toolExecutor == nil {
		subsystem := guimcp.New(s.Core())
		server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "coregui-chat", Version: "0.1.0"}, nil)
		subsystem.RegisterTools(server)
		s.toolExecutor = adapter{subsystem: subsystem}
	}
	registerMCPToolActions(s.Core(), s.toolExecutor)
	s.toolExecutor = newActionToolExecutor(s.Core(), s.toolExecutor)
	s.toolHandler = NewToolCallHandler(s.toolExecutor)
	s.Core().RegisterQuery(s.handleQuery)
	s.registerActions()
	return core.Result{OK: true}
}

func (s *Service) HandleIPCEvents(_ *core.Core, _ core.Message) core.Result {
	return core.Result{OK: true}
}

func (s *Service) handleQuery(_ *core.Core, q core.Query) core.Result {
	switch typed := q.(type) {
	case QueryHistory:
		conv, err := s.getConversation(typed.ID, typed.ConversationID)
		return core.Result{}.New(conv, err)
	case QueryModels:
		return core.Result{Value: s.Models(), OK: true}
	case QuerySettings:
		return core.Result{Value: s.loadSettings(), OK: true}
	case QuerySettingsDefaults:
		return core.Result{Value: DefaultSettings(), OK: true}
	case QueryConversationList:
		conversations, err := s.listConversationSummaries()
		return core.Result{}.New(conversations, err)
	case QueryConversationGet:
		conv, err := s.getConversation(typed.ID, typed.ConversationID)
		return core.Result{}.New(conv, err)
	case QueryConversationSearch:
		results, err := s.searchConversationSummaries(typed.Query)
		return core.Result{}.New(results, err)
	default:
		return core.Result{}
	}
}

func (s *Service) registerActions() {
	c := s.Core()
	c.Action("gui.chat.send", func(ctx context.Context, opts core.Options) core.Result {
		input, err := decodeInput[sendInput](opts)
		if err != nil {
			return core.Result{Value: err, OK: false}
		}
		messageID, err := s.Send(ctx, input)
		return core.Result{}.New(messageID, err)
	})
	c.Action("gui.chat.clear", func(_ context.Context, opts core.Options) core.Result {
		input, err := decodeInput[conversationInput](opts)
		if err != nil {
			return core.Result{Value: err, OK: false}
		}
		conv, err := s.clearConversation(input.ID, input.ConversationID)
		return core.Result{}.New(conv, err)
	})
	c.Action("gui.chat.history", func(_ context.Context, opts core.Options) core.Result {
		input, err := decodeInput[historyInput](opts)
		if err != nil {
			return core.Result{Value: err, OK: false}
		}
		messages, err := s.History(coalesce(input.ID, input.ConversationID), input.Limit)
		return core.Result{}.New(messages, err)
	})
	c.Action("gui.chat.models", func(_ context.Context, _ core.Options) core.Result {
		return core.Result{Value: s.Models(), OK: true}
	})
	c.Action("gui.chat.select_model", func(_ context.Context, opts core.Options) core.Result {
		input, err := decodeInput[selectModelInput](opts)
		if err != nil {
			return core.Result{Value: err, OK: false}
		}
		settings, err := s.SelectModel(input)
		return core.Result{}.New(settings, err)
	})
	c.Action("gui.chat.settings.save", func(_ context.Context, opts core.Options) core.Result {
		settings, err := decodeInput[ChatSettings](opts)
		if err != nil {
			return core.Result{Value: err, OK: false}
		}
		err = s.saveSettings(settings)
		return core.Result{}.New(settings, err)
	})
	c.Action("gui.chat.settings.load", func(_ context.Context, _ core.Options) core.Result {
		return core.Result{Value: s.loadSettings(), OK: true}
	})
	c.Action("gui.chat.settings.defaults", func(_ context.Context, _ core.Options) core.Result {
		return core.Result{Value: DefaultSettings(), OK: true}
	})
	c.Action("gui.chat.settings.reset", func(_ context.Context, _ core.Options) core.Result {
		settings := DefaultSettings()
		err := s.saveSettings(settings)
		return core.Result{}.New(settings, err)
	})
	c.Action("gui.chat.conversations.list", func(_ context.Context, _ core.Options) core.Result {
		conversations, err := s.ListConversations()
		return core.Result{}.New(conversations, err)
	})
	loadConversation := func(_ context.Context, opts core.Options) core.Result {
		input, err := decodeInput[conversationInput](opts)
		if err != nil {
			return core.Result{Value: err, OK: false}
		}
		conv, err := s.LoadConversation(coalesce(input.ID, input.ConversationID))
		return core.Result{}.New(conv, err)
	}
	c.Action("gui.chat.conversations.load", loadConversation)
	c.Action("gui.chat.conversations.get", loadConversation)
	c.Action("gui.chat.conversations.delete", func(_ context.Context, opts core.Options) core.Result {
		input, err := decodeInput[conversationInput](opts)
		if err != nil {
			return core.Result{Value: err, OK: false}
		}
		err = s.DeleteConversation(coalesce(input.ID, input.ConversationID))
		return core.Result{}.New(true, err)
	})
	c.Action("gui.chat.conversations.search", func(_ context.Context, opts core.Options) core.Result {
		input, err := decodeInput[searchInput](opts)
		if err != nil {
			return core.Result{Value: err, OK: false}
		}
		results, err := s.searchConversationSummaries(input.Query)
		return core.Result{}.New(results, err)
	})
	c.Action("gui.chat.conversations.new", func(_ context.Context, _ core.Options) core.Result {
		conv, err := s.createConversation()
		return core.Result{}.New(conv, err)
	})
	c.Action("gui.chat.conversation.save", func(_ context.Context, opts core.Options) core.Result {
		conv, err := decodeInput[Conversation](opts)
		if err != nil {
			return core.Result{Value: err, OK: false}
		}
		if core.Trim(conv.ID) == "" {
			return core.Result{Value: core.E("chat.conversation.save", "conversation id is required", nil), OK: false}
		}
		saved, err := s.saveConversation(conv)
		if err != nil {
			return core.Result{Value: err, OK: false}
		}
		s.emit(ActionConversationUpdated{Conversation: saved})
		return core.Result{Value: saved, OK: true}
	})
	c.Action("gui.chat.conversations.rename", func(_ context.Context, opts core.Options) core.Result {
		input, err := decodeInput[renameInput](opts)
		if err != nil {
			return core.Result{Value: err, OK: false}
		}
		conv, err := s.renameConversation(input.ID, input.Title)
		return core.Result{}.New(conv, err)
	})
	c.Action("gui.chat.conversations.export", func(_ context.Context, opts core.Options) core.Result {
		input, err := decodeInput[conversationInput](opts)
		if err != nil {
			return core.Result{Value: err, OK: false}
		}
		markdown, err := s.exportConversation(coalesce(input.ID, input.ConversationID))
		return core.Result{}.New(markdown, err)
	})
	c.Action("gui.chat.attach_image", func(_ context.Context, opts core.Options) core.Result {
		input, err := decodeInput[attachImageInput](opts)
		if err != nil {
			return core.Result{Value: err, OK: false}
		}
		if err := validateImageAttachment(input.ImageAttachment); err != nil {
			return core.Result{Value: err, OK: false}
		}
		s.queueAttachment(coalesce(input.ConversationID, "draft"), input.ImageAttachment)
		return core.Result{Value: input.ImageAttachment, OK: true}
	})
	c.Action("gui.chat.attach_image_file", func(_ context.Context, opts core.Options) core.Result {
		input, err := decodeInput[attachImageFileInput](opts)
		if err != nil {
			return core.Result{Value: err, OK: false}
		}
		attachment, err := imageAttachmentFromFile(input.Path)
		if err != nil {
			return core.Result{Value: err, OK: false}
		}
		s.queueAttachment(coalesce(input.ConversationID, "draft"), attachment)
		return core.Result{Value: attachment, OK: true}
	})
	c.Action("gui.chat.remove_image", func(_ context.Context, opts core.Options) core.Result {
		input, err := decodeInput[removeImageInput](opts)
		if err != nil {
			return core.Result{Value: err, OK: false}
		}
		attachment, err := s.removeAttachment(coalesce(input.ConversationID, "draft"), input.Index)
		return core.Result{}.New(attachment, err)
	})
	c.Action("gui.chat.thinking.start", func(_ context.Context, opts core.Options) core.Result {
		input, err := decodeInput[thinkingInput](opts)
		if err != nil {
			return core.Result{Value: err, OK: false}
		}
		state, err := s.StartThinking(input)
		return core.Result{}.New(state, err)
	})
	c.Action("gui.chat.thinking.append", func(_ context.Context, opts core.Options) core.Result {
		input, err := decodeInput[thinkingInput](opts)
		if err != nil {
			return core.Result{Value: err, OK: false}
		}
		if core.Trim(input.ConversationID) == "" {
			return core.Result{Value: core.E("chat.thinking.append", "conversation id is required", nil), OK: false}
		}
		s.emit(ActionThinkingAppended{
			ConversationID: input.ConversationID,
			MessageID:      input.MessageID,
			Content:        input.Content,
		})
		s.appendThinking(input.ConversationID, input.Content)
		return core.Result{Value: input.Content, OK: true}
	})
	stopThinking := func(_ context.Context, opts core.Options) core.Result {
		input, err := decodeInput[thinkingInput](opts)
		if err != nil {
			return core.Result{Value: err, OK: false}
		}
		state, err := s.StopThinking(input)
		return core.Result{}.New(state, err)
	}
	c.Action("gui.chat.thinking.stop", stopThinking)
	c.Action("gui.chat.thinking.end", stopThinking)
}

func decodeInput[T any](opts core.Options) (T, resultFailure) {
	var input T
	if task := opts.Get("task"); task.OK {
		if typed, ok := task.Value.(T); ok {
			return typed, nil
		}
	}

	items := make(map[string]any, opts.Len())
	for _, item := range opts.Items() {
		items[item.Key] = item.Value
	}
	if len(items) == 0 {
		return input, nil
	}

	result := core.JSONUnmarshalString(core.JSONMarshalString(items), &input)
	if !result.OK {
		if err, ok := result.Value.(resultFailure); ok {
			return input, err
		}
		return input, core.E("chat.decodeInput", "failed to decode action input", nil)
	}
	return input, nil
}

func defaultModelRoots() []string {
	roots := []string{core.PathJoin(core.Env("DIR_HOME"), ".core", "models")}
	if env := core.Trim(core.Env("CORE_MODELS_DIR")); env != "" {
		roots = append(roots, env)
	}
	return roots
}

func (s *Service) now() time.Time {
	if s.options.Now != nil {
		return s.options.Now()
	}
	return time.Now()
}

func (s *Service) Send(ctx context.Context, input sendInput) (string, resultFailure) {
	return s.send(ctx, input)
}

func (s *Service) History(conversationID string, limit int) ([]Message, resultFailure) {
	if limit < 0 {
		return nil, core.E("chat.history", "limit must be greater than or equal to zero", nil)
	}
	conv, err := s.LoadConversation(conversationID)
	if err != nil {
		return nil, err
	}
	if limit == 0 || limit >= len(conv.Messages) {
		return slices.Clone(conv.Messages), nil
	}
	return slices.Clone(conv.Messages[len(conv.Messages)-limit:]), nil
}

func (s *Service) Models() []ModelEntry {
	models := s.discoverModels()
	activeName := s.resolveModel("", s.loadSettings().DefaultModel)
	if len(models) == 0 {
		return []ModelEntry{{
			Name:   activeName,
			Size:   0,
			Status: "active",
			Loaded: true,
		}}
	}

	for index := range models {
		models[index].Size = models[index].SizeBytes
		switch {
		case equalFold(models[index].Name, activeName):
			models[index].Loaded = true
			models[index].Status = "active"
		case models[index].Loaded:
			models[index].Status = "loaded"
		default:
			models[index].Status = "available"
		}
	}
	return models
}

func (s *Service) saveSettings(settings ChatSettings) resultFailure {
	if err := s.validateSettings(settings); err != nil {
		return err
	}
	payload := core.JSONMarshalString(settings)
	return s.store.set(settingsGroup, settingsKey, payload)
}

func (s *Service) loadSettings() ChatSettings {
	settings := DefaultSettings()
	if s.store == nil {
		return settings
	}
	payload, err := s.store.get(settingsGroup, settingsKey)
	if err != nil {
		return settings
	}
	if r := core.JSONUnmarshalString(payload, &settings); !r.OK {
		return DefaultSettings()
	}
	return settings
}

func (s *Service) SelectModel(input selectModelInput) (ChatSettings, resultFailure) {
	modelName := coalesce(input.Name, input.Model)
	if err := s.validateModelName(modelName); err != nil {
		return ChatSettings{}, err
	}
	settings := s.loadSettings()
	settings.DefaultModel = modelName
	if err := s.saveSettings(settings); err != nil {
		return ChatSettings{}, err
	}

	targetConversation := coalesce(input.ConversationID, input.ID)
	if targetConversation == "" {
		return settings, nil
	}

	conv, err := s.loadConversation(targetConversation)
	if err != nil {
		return ChatSettings{}, err
	}
	conv.Model = modelName
	conv, err = s.saveConversation(conv)
	if err != nil {
		return ChatSettings{}, err
	}
	s.emit(ActionConversationUpdated{Conversation: conv})
	return settings, nil
}

func (s *Service) ListConversations() ([]Conversation, resultFailure) {
	return s.listConversations()
}

func (s *Service) LoadConversation(id string) (Conversation, resultFailure) {
	return s.getConversation(id, "")
}

func (s *Service) DeleteConversation(id string) resultFailure {
	return s.deleteConversation(id)
}

func (s *Service) StartThinking(input thinkingInput) (ThinkingState, resultFailure) {
	if core.Trim(input.ConversationID) == "" {
		return ThinkingState{}, core.E("chat.thinking.start", "conversation id is required", nil)
	}

	state := ThinkingState{
		Active:    true,
		StartedAt: input.StartedAt,
	}
	if state.StartedAt.IsZero() {
		state.StartedAt = s.now()
	}

	s.mu.Lock()
	s.thinkingStates[input.ConversationID] = state
	s.mu.Unlock()

	s.emit(ActionThinkingStarted{
		ConversationID: input.ConversationID,
		MessageID:      input.MessageID,
		StartedAt:      state.StartedAt,
	})
	return state, nil
}

func (s *Service) StopThinking(input thinkingInput) (ThinkingState, resultFailure) {
	if core.Trim(input.ConversationID) == "" {
		return ThinkingState{}, core.E("chat.thinking.stop", "conversation id is required", nil)
	}

	s.mu.Lock()
	state, ok := s.thinkingStates[input.ConversationID]
	delete(s.thinkingStates, input.ConversationID)
	s.mu.Unlock()

	if !ok && !input.StartedAt.IsZero() {
		state.StartedAt = input.StartedAt
	}
	if state.StartedAt.IsZero() {
		state.StartedAt = s.now()
	}

	state.Active = false
	state.Content = core.Trim(state.Content)
	state.EndedAt = s.now()
	state.DurationMS = input.DurationMS
	if state.DurationMS == 0 {
		state.DurationMS = max(state.EndedAt.Sub(state.StartedAt).Milliseconds(), 0)
	}

	s.emit(ActionThinkingEnded{
		ConversationID: input.ConversationID,
		MessageID:      input.MessageID,
		DurationMS:     state.DurationMS,
	})
	return state, nil
}

func (s *Service) appendThinking(conversationID, content string) {
	key := core.Trim(conversationID)
	if key == "" || core.Trim(content) == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.thinkingStates[key]
	if state.StartedAt.IsZero() {
		state.StartedAt = s.now()
	}
	state.Active = true
	state.Content += content
	s.thinkingStates[key] = state
}

func (s *Service) saveConversation(conv Conversation) (Conversation, resultFailure) {
	if err := s.validateConversation(conv); err != nil {
		return Conversation{}, err
	}
	if conv.CreatedAt.IsZero() {
		conv.CreatedAt = s.now()
	}
	if core.Trim(conv.Title) == "" {
		if len(conv.Messages) > 0 && core.Trim(conv.Messages[0].Content) != "" {
			conv.Title = titleFrom(conv.Messages[0].Content)
		} else {
			conv.Title = "New Chat"
		}
	}
	conv.UpdatedAt = s.now()
	payload := core.JSONMarshalString(conv)
	return conv, s.store.set(conversationsGroup, conv.ID, payload)
}

func (s *Service) loadConversation(id string) (Conversation, resultFailure) {
	payload, err := s.store.get(conversationsGroup, id)
	if err != nil {
		return Conversation{}, err
	}
	var conv Conversation
	result := core.JSONUnmarshalString(payload, &conv)
	if !result.OK {
		if decodeErr, ok := result.Value.(resultFailure); ok {
			return Conversation{}, decodeErr
		}
		return Conversation{}, core.E("chat.loadConversation", "failed to decode conversation", nil)
	}
	return conv, nil
}

func (s *Service) listConversations() ([]Conversation, resultFailure) {
	if s.store == nil {
		return nil, nil
	}
	items, err := s.store.getAll(conversationsGroup)
	if err != nil {
		return nil, err
	}

	conversations := make([]Conversation, 0, len(items))
	for _, payload := range items {
		var conv Conversation
		if result := core.JSONUnmarshalString(payload, &conv); result.OK {
			conversations = append(conversations, conv)
		}
	}
	sort.Slice(conversations, func(i, j int) bool {
		return conversations[i].UpdatedAt.After(conversations[j].UpdatedAt)
	})
	return conversations, nil
}

func (s *Service) listConversationSummaries() ([]ConversationSummary, resultFailure) {
	conversations, err := s.listConversations()
	if err != nil {
		return nil, err
	}

	summaries := make([]ConversationSummary, 0, len(conversations))
	for _, conv := range conversations {
		summaries = append(summaries, conv.Summary())
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})
	return summaries, nil
}

func (s *Service) searchConversationSummaries(query string) ([]ConversationSummary, resultFailure) {
	query = core.Trim(core.Lower(query))
	summaries, err := s.listConversationSummaries()
	if err != nil || query == "" {
		return summaries, err
	}

	items, err := s.store.getAll(conversationsGroup)
	if err != nil {
		return nil, err
	}
	matches := make([]ConversationSummary, 0)
	for _, payload := range items {
		var conv Conversation
		if result := core.JSONUnmarshalString(payload, &conv); !result.OK {
			continue
		}
		if core.Contains(conversationSearchText(conv), query) {
			matches = append(matches, conv.Summary())
			continue
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].UpdatedAt.After(matches[j].UpdatedAt)
	})
	return matches, nil
}

func conversationSearchText(conv Conversation) string {
	builder := core.NewBuilder()
	appendText := func(value string) {
		trimmed := core.Trim(value)
		if trimmed == "" {
			return
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(core.Lower(trimmed))
	}

	appendText(conv.Title)
	appendText(conv.Model)
	if conv.Settings != nil {
		appendText(conv.Settings.SystemPrompt)
		appendText(conv.Settings.DefaultModel)
	}
	for _, message := range conv.Messages {
		appendText(message.Content)
		appendText(message.Role)
		appendText(message.Model)
		appendText(message.FinishReason)
		if message.Thinking != nil {
			appendText(message.Thinking.Content)
		}
		for _, attachment := range message.Attachments {
			appendText(attachment.Filename)
			appendText(attachment.MimeType)
		}
		for _, call := range message.ToolCalls {
			appendText(call.ID)
			appendText(call.Name)
			appendText(core.JSONMarshalString(call.Arguments))
		}
		for _, result := range message.ToolResults {
			appendText(result.ToolCallID)
			appendText(result.Content)
		}
	}
	return builder.String()
}

func (s *Service) createConversation() (Conversation, resultFailure) {
	settings := s.loadSettings()
	now := s.now()
	conv := Conversation{
		ID:        "conv-" + strconv.FormatInt(now.UnixNano(), 36),
		Title:     "New Chat",
		Model:     settings.DefaultModel,
		CreatedAt: now,
		UpdatedAt: now,
		Messages:  nil,
	}
	conv, err := s.saveConversation(conv)
	if err != nil {
		return Conversation{}, err
	}
	s.emit(ActionConversationCreated{Conversation: conv})
	return conv, nil
}

func (s *Service) getConversation(id, conversationID string) (Conversation, resultFailure) {
	target := coalesce(id, conversationID)
	if target == "" {
		return Conversation{}, core.E("chat.getConversation", "conversation id is required", nil)
	}
	return s.loadConversation(target)
}

func (s *Service) renameConversation(id, title string) (Conversation, resultFailure) {
	conv, err := s.loadConversation(id)
	if err != nil {
		return Conversation{}, err
	}
	conv.Title = core.Trim(title)
	if conv.Title == "" {
		conv.Title = "New Chat"
	}
	conv, err = s.saveConversation(conv)
	if err != nil {
		return Conversation{}, err
	}
	s.emit(ActionConversationUpdated{Conversation: conv})
	return conv, nil
}

func (s *Service) clearConversation(id, conversationID string) (Conversation, resultFailure) {
	conv, err := s.getConversation(id, conversationID)
	if err != nil {
		return Conversation{}, err
	}
	conv.Messages = nil
	conv.UpdatedAt = s.now()
	conv, err = s.saveConversation(conv)
	if err != nil {
		return Conversation{}, err
	}
	s.clearQueuedAttachments(conv.ID)
	s.emit(ActionConversationCleared{ConversationID: conv.ID})
	s.emit(ActionConversationUpdated{Conversation: conv})
	return conv, nil
}

func (s *Service) deleteConversation(id string) resultFailure {
	if id == "" {
		return core.E("chat.deleteConversation", "conversation id is required", nil)
	}
	if err := s.store.delete(conversationsGroup, id); err != nil {
		return err
	}
	s.clearQueuedAttachments(id)
	s.emit(ActionConversationDeleted{ConversationID: id})
	return nil
}

func (s *Service) exportConversation(id string) (string, resultFailure) {
	conv, err := s.loadConversation(id)
	if err != nil {
		return "", err
	}
	builder := core.NewBuilder()
	builder.WriteString("# ")
	builder.WriteString(conv.Title)
	builder.WriteString("\n\n")
	titleCaser := cases.Title(language.Und)
	for _, message := range conv.Messages {
		builder.WriteString("## ")
		builder.WriteString(titleCaser.String(message.Role))
		builder.WriteString("\n\n")
		if message.Content != "" {
			builder.WriteString(message.Content)
			builder.WriteString("\n\n")
		}
		for _, result := range message.ToolResults {
			builder.WriteString("> Tool Result (`")
			builder.WriteString(result.ToolCallID)
			builder.WriteString("`)\n>\n> ")
			builder.WriteString(core.Replace(result.Content, "\n", "\n> "))
			builder.WriteString("\n\n")
		}
	}
	return builder.String(), nil
}

func (s *Service) queueAttachment(conversationID string, attachment ImageAttachment) {
	key := coalesce(conversationID, "draft")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingAttachments[key] = append(s.pendingAttachments[key], attachment)
	s.emit(ActionImageQueued{ConversationID: key, Attachment: attachment})
}

func (s *Service) drainAttachments(conversationID string) []ImageAttachment {
	s.mu.Lock()
	defer s.mu.Unlock()
	var attachments []ImageAttachment
	keys := []string{conversationID}
	if conversationID != "draft" {
		keys = append(keys, "draft")
	}
	for _, key := range keys {
		if key == "" {
			continue
		}
		attachments = append(attachments, s.pendingAttachments[key]...)
		delete(s.pendingAttachments, key)
	}
	return attachments
}

// removeAttachment removes a queued image by index from the pending attachment queue.
// Use: removed, _ := service.removeAttachment("draft", 0)
func (s *Service) removeAttachment(conversationID string, index int) (ImageAttachment, resultFailure) {
	key := coalesce(conversationID, "draft")
	if index < 0 {
		return ImageAttachment{}, core.E("chat.removeAttachment", "attachment index must be non-negative", nil)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	attachments := s.pendingAttachments[key]
	if index >= len(attachments) {
		return ImageAttachment{}, core.E("chat.removeAttachment", "attachment index is out of range", nil)
	}

	removed := attachments[index]
	next := append(attachments[:index:index], attachments[index+1:]...)
	if len(next) == 0 {
		delete(s.pendingAttachments, key)
	} else {
		s.pendingAttachments[key] = next
	}
	return removed, nil
}

func (s *Service) clearQueuedAttachments(conversationID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pendingAttachments, conversationID)
	if conversationID != "draft" {
		delete(s.pendingAttachments, "draft")
	}
}

func (s *Service) mergedSettings(global ChatSettings, override *ChatSettings) ChatSettings {
	if override == nil {
		return global
	}
	merged := global
	if override.Temperature != 0 {
		merged.Temperature = override.Temperature
	}
	if override.TopP != 0 {
		merged.TopP = override.TopP
	}
	if override.TopK != 0 {
		merged.TopK = override.TopK
	}
	if override.MaxTokens != 0 {
		merged.MaxTokens = override.MaxTokens
	}
	if override.ContextWindow != 0 {
		merged.ContextWindow = override.ContextWindow
	}
	if override.SystemPrompt != "" {
		merged.SystemPrompt = override.SystemPrompt
	}
	if override.DefaultModel != "" {
		merged.DefaultModel = override.DefaultModel
	}
	return merged
}

func (s *Service) send(ctx context.Context, input sendInput) (string, resultFailure) {
	if core.Trim(input.Content) == "" && !s.hasPendingAttachments(input.ConversationID) {
		return "", core.E("chat.send", "message content is required", nil)
	}

	settings := s.loadSettings()
	var (
		conv                   Conversation
		err                    resultFailure
		created                bool
		lastAssistantMessageID string
	)
	if input.ConversationID != "" {
		conv, err = s.loadConversation(input.ConversationID)
	} else {
		conv, err = s.createConversation()
		created = true
	}
	if err != nil {
		return "", err
	}

	attachments := s.drainAttachments(conv.ID)
	if len(attachments) == 0 && created {
		attachments = s.drainAttachments("draft")
	}

	now := s.now()
	if modelName := core.Trim(input.Model); modelName != "" {
		if err := s.validateModelName(modelName); err != nil {
			return "", err
		}
		conv.Model = modelName
	}
	conv.Model = s.resolveModel(conv.Model, settings.DefaultModel)
	userMessage := ChatMessage{
		ID:          "msg-" + strconv.FormatInt(now.UnixNano(), 36),
		Role:        "user",
		Content:     input.Content,
		CreatedAt:   now,
		Model:       conv.Model,
		Attachments: attachments,
	}
	conv.Messages = append(conv.Messages, userMessage)
	if conv.Title == "" || conv.Title == "New Chat" {
		conv.Title = titleFrom(input.Content)
	}
	conv, err = s.saveConversation(conv)
	if err != nil {
		return "", err
	}
	s.emit(ActionMessageAdded{ConversationID: conv.ID, Message: userMessage})
	s.emit(ActionConversationUpdated{Conversation: conv})

	for range maxToolRounds {
		effectiveSettings := s.mergedSettings(settings, conv.Settings)
		conv.Model = s.resolveModel(conv.Model, effectiveSettings.DefaultModel)
		if err := s.validateAttachmentsForModel(conv.Model, attachmentsForConversationTurn(conv.Messages)); err != nil {
			return "", err
		}

		assistantMessage, err := s.streamAssistant(ctx, conv, effectiveSettings)
		if err != nil {
			return "", err
		}
		assistantMessage = s.withInlineToolCall(conv.ID, assistantMessage)
		lastAssistantMessageID = assistantMessage.ID
		if hasRenderableContent(assistantMessage) {
			conv.Messages = append(conv.Messages, assistantMessage)
			conv, err = s.saveConversation(conv)
			if err != nil {
				return "", err
			}
			s.emit(ActionMessageAdded{ConversationID: conv.ID, Message: assistantMessage})
			s.emit(ActionConversationUpdated{Conversation: conv})
		}

		if len(assistantMessage.ToolCalls) == 0 {
			break
		}
		if s.toolHandler == nil {
			break
		}

		for _, call := range assistantMessage.ToolCalls {
			resultContent, err := s.toolHandler.OnToolCall(ctx, call)
			result := ToolResult{
				ToolCallID: call.ID,
				Content:    renderToolResultContent(resultContent),
			}
			if err != nil {
				result.Content = err.Error()
			}
			toolMessage := ChatMessage{
				ID:          "tool-" + strconv.FormatInt(s.now().UnixNano(), 36),
				Role:        "tool",
				Content:     result.Content,
				CreatedAt:   s.now(),
				Model:       conv.Model,
				ToolCallID:  result.ToolCallID,
				ToolResults: []ToolResult{result},
			}
			conv.Messages = append(conv.Messages, toolMessage)
			s.emit(ActionToolResultReady{ConversationID: conv.ID, MessageID: assistantMessage.ID, Result: result})
			s.emit(ActionMessageAdded{ConversationID: conv.ID, Message: toolMessage})
		}
		conv, err = s.saveConversation(conv)
		if err != nil {
			return "", err
		}
		s.emit(ActionConversationUpdated{Conversation: conv})
	}

	if lastAssistantMessageID == "" {
		lastAssistantMessageID = userMessage.ID
	}
	return lastAssistantMessageID, nil
}

func (s *Service) hasPendingAttachments(conversationID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if attachments := s.pendingAttachments[coalesce(conversationID, "draft")]; len(attachments) > 0 {
		return true
	}
	if conversationID != "" && len(s.pendingAttachments["draft"]) > 0 {
		return true
	}
	return false
}

func (s *Service) streamAssistant(ctx context.Context, conv Conversation, settings ChatSettings) (ChatMessage, resultFailure) {
	messageID := "msg-" + strconv.FormatInt(s.now().UnixNano(), 36)
	requestBody := s.buildCompletionRequest(conv, settings)
	payload := core.JSONMarshalString(requestBody)

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, trimRight(s.options.APIURL, "/")+"/v1/chat/completions", core.NewBufferString(payload))
	if err != nil {
		return ChatMessage{}, err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := s.httpClient.Do(request)
	if err != nil {
		return ChatMessage{}, err
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(response.Body)
		return ChatMessage{}, core.E("chat.streamAssistant", core.Trim(string(body)), nil)
	}

	// TODO(mantis-14): switch these callbacks to a dedicated GUI stream group when one exists.
	renderer := NewStreamRenderer(StreamCallbacks{
		OnStart: func(streamID string) {
			s.emit(ActionStreamStarted{ConversationID: conv.ID, MessageID: messageID, StreamID: streamID})
		},
		OnToken: func(content string) {
			s.emit(ActionTokenAppended{ConversationID: conv.ID, MessageID: messageID, Content: content})
		},
		OnThinkingStart: func(state ThinkingState) {
			s.emit(ActionThinkingStarted{ConversationID: conv.ID, MessageID: messageID, StartedAt: state.StartedAt})
		},
		OnThinkingAppend: func(content string) {
			s.emit(ActionThinkingAppended{ConversationID: conv.ID, MessageID: messageID, Content: content})
		},
		OnThinkingEnd: func(state ThinkingState) {
			s.emit(ActionThinkingEnded{ConversationID: conv.ID, MessageID: messageID, DurationMS: state.DurationMS})
		},
		OnToolCall: func(call ToolCall) {
			s.emit(ActionToolCallStarted{ConversationID: conv.ID, MessageID: messageID, Call: call})
		},
		OnFinish: func(reason string) {
			s.emit(ActionStreamFinished{ConversationID: conv.ID, MessageID: messageID, FinishReason: reason})
		},
	})
	if err := renderer.Render(response.Body); err != nil {
		return ChatMessage{}, err
	}
	return renderer.Message(messageID, conv.Model, s.now()), nil
}

func (s *Service) withInlineToolCall(conversationID string, message ChatMessage) ChatMessage {
	if len(message.ToolCalls) > 0 {
		return message
	}

	call, ok, err := parseInlineToolCall(message.Content)
	if err != nil {
		coreutil.LogWarn(s.Core(), err, "chat.tool_call", "malformed inline tool_call ignored")
		return message
	}
	if !ok {
		return message
	}
	if call.ID == "" {
		call.ID = "call-" + strconv.FormatInt(s.now().UnixNano(), 36)
	}
	message.Content = ""
	message.ToolCalls = []ToolCall{call}
	message.FinishReason = "tool_calls"
	s.emit(ActionToolCallStarted{ConversationID: conversationID, MessageID: message.ID, Call: call})
	return message
}

func (s *Service) buildCompletionRequest(conv Conversation, settings ChatSettings) openAIRequest {
	request := openAIRequest{
		Model:       s.resolveModel(conv.Model, settings.DefaultModel),
		Messages:    make([]openAIMessage, 0, len(conv.Messages)+1),
		Temperature: settings.Temperature,
		TopP:        settings.TopP,
		TopK:        settings.TopK,
		MaxTokens:   settings.MaxTokens,
		Stream:      true,
	}

	systemPrompt := s.buildSystemPrompt(settings)
	if tools := s.buildToolSpecs(); len(tools) > 0 {
		request.Tools = tools
		request.ToolChoice = "auto"
	}
	if systemPrompt != "" {
		request.Messages = append(request.Messages, openAIMessage{
			Role:    "system",
			Content: systemPrompt,
		})
	}

	for _, message := range conv.Messages {
		request.Messages = append(request.Messages, buildAPIMessage(message))
	}

	return request
}

func (s *Service) buildSystemPrompt(settings ChatSettings) string {
	systemPrompt := core.Trim(settings.SystemPrompt)
	if s.toolHandler == nil {
		return systemPrompt
	}
	manifest := s.toolHandler.BuildToolManifest()
	if manifest == "" {
		return systemPrompt
	}
	if systemPrompt != "" {
		return manifest + "\n\n" + systemPrompt
	}
	return manifest
}

func (s *Service) buildToolSpecs() []openAIToolSpec {
	if s.toolExecutor == nil {
		return nil
	}
	manifest := s.toolExecutor.Manifest()
	if len(manifest) == 0 {
		return nil
	}
	specs := make([]openAIToolSpec, 0, len(manifest))
	for _, tool := range manifest {
		specs = append(specs, openAIToolSpec{
			Type: "function",
			Function: openAIFunctionSpec{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
	}
	return specs
}

func buildAPIMessage(message ChatMessage) openAIMessage {
	apiMessage := openAIMessage{Role: message.Role}
	switch message.Role {
	case "user":
		apiMessage.Content = renderUserContent(message)
	case "assistant":
		apiMessage.Content = message.Content
		if len(message.ToolCalls) > 0 {
			apiMessage.ToolCalls = make([]openAIToolCall, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				apiMessage.ToolCalls = append(apiMessage.ToolCalls, openAIToolCall{
					ID:   call.ID,
					Type: "function",
					Function: openAIFunctionCall{
						Name:      call.Name,
						Arguments: core.JSONMarshalString(call.Arguments),
					},
				})
			}
		}
	case "tool":
		apiMessage.Content = message.Content
		apiMessage.ToolCallID = message.ToolCallID
	default:
		apiMessage.Content = message.Content
	}
	return apiMessage
}

func (s *Service) resolveModel(current, configured string) string {
	if current = core.Trim(current); current != "" {
		return current
	}
	if configured = core.Trim(configured); configured != "" {
		return configured
	}
	models := s.discoverModels()
	if len(models) > 0 {
		return models[0].Name
	}
	return "default"
}

func renderUserContent(message ChatMessage) any {
	if len(message.Attachments) == 0 {
		return message.Content
	}
	parts := []map[string]any{
		{"type": "text", "text": message.Content},
	}
	for _, attachment := range message.Attachments {
		parts = append(parts, map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"url": "data:" + attachment.MimeType + ";base64," + attachment.Data,
			},
		})
	}
	return parts
}

func hasRenderableContent(message ChatMessage) bool {
	return core.Trim(message.Content) != "" || message.Thinking != nil || len(message.ToolCalls) > 0
}

func titleFrom(content string) string {
	title := core.Trim(content)
	if title == "" {
		return "New Chat"
	}
	runes := []rune(title)
	if len(runes) > 50 {
		return string(runes[:50])
	}
	return title
}

func coalesce(values ...string) string {
	for _, value := range values {
		if core.Trim(value) != "" {
			return value
		}
	}
	return ""
}

func (s *Service) emit(message any) {
	if message == nil {
		return
	}
	coreutil.DispatchAction(s.Core(), "chat.emit", message)
}

func (s *Service) discoverModels() []ModelEntry {
	settings := s.loadSettings()
	models := map[string]ModelEntry{}
	for _, root := range s.options.ModelRoots {
		for _, model := range discoverModelsOnDisk(root) {
			models[model.Name] = model
		}
	}

	configPath := core.PathJoin(core.Env("DIR_HOME"), ".core", "models.yaml")
	if payload, err := coreReadFile(configPath); err == nil {
		var configured configuredModels
		if err := yaml.Unmarshal(payload, &configured); err == nil {
			defaultModel := coalesce(configured.DefaultModel, configured.Default, settings.DefaultModel)
			for _, item := range configured.Models {
				name := coalesce(item.Name, core.PathBase(item.Path))
				entry := models[name]
				entry.Name = name
				entry.Architecture = coalesce(item.Architecture, entry.Architecture)
				entry.Backend = coalesce(item.Backend, entry.Backend, "local")
				if item.SupportsVision != nil {
					entry.SupportsVision = *item.SupportsVision
				} else if entry.Architecture != "" {
					entry.SupportsVision = architectureSupportsVision(entry.Architecture)
				}
				if entry.SizeBytes == 0 && item.Path != "" {
					entry.SizeBytes = directorySize(item.Path)
				}
				entry.Loaded = name == defaultModel
				models[name] = entry
			}
		}
	}

	names := make([]string, 0, len(models))
	for name := range models {
		names = append(names, name)
	}
	slices.Sort(names)
	activeModel := core.Trim(settings.DefaultModel)
	if activeModel == "" && len(names) > 0 {
		activeModel = names[0]
	}
	results := make([]ModelEntry, 0, len(names))
	for _, name := range names {
		entry := models[name]
		entry.Size = entry.SizeBytes
		switch {
		case equalFold(entry.Name, activeModel):
			entry.Loaded = true
			entry.Status = "active"
		case entry.Loaded:
			entry.Status = "loaded"
		default:
			entry.Status = "available"
		}
		results = append(results, entry)
	}
	return results
}

func (s *Service) validateSettings(settings ChatSettings) resultFailure {
	if settings.Temperature < 0 || settings.Temperature > 2 {
		return core.E("chat.settings.save", "temperature must be between 0.0 and 2.0", nil)
	}
	if settings.TopP < 0 || settings.TopP > 1 {
		return core.E("chat.settings.save", "top_p must be between 0.0 and 1.0", nil)
	}
	if settings.TopK < 0 || settings.TopK > 200 {
		return core.E("chat.settings.save", "top_k must be between 0 and 200", nil)
	}
	if settings.MaxTokens < 64 || settings.MaxTokens > 32768 {
		return core.E("chat.settings.save", "max_tokens must be between 64 and 32768", nil)
	}
	if !validContextWindow(settings.ContextWindow) {
		return core.E("chat.settings.save", "context_window must be one of 2048, 4096, 8192, 16384, or 32768", nil)
	}
	if err := s.validateOptionalModelName(settings.DefaultModel); err != nil {
		return err
	}
	return nil
}

func validContextWindow(value int) bool {
	switch value {
	case 2048, 4096, 8192, 16384, 32768:
		return true
	default:
		return false
	}
}

func (s *Service) validateConversation(conv Conversation) resultFailure {
	if core.Trim(conv.ID) == "" {
		return core.E("chat.saveConversation", "conversation id is required", nil)
	}
	if err := s.validateOptionalModelName(conv.Model); err != nil {
		return err
	}
	if conv.Settings != nil {
		if err := s.validateSettings(*conv.Settings); err != nil {
			return err
		}
	}
	for _, message := range conv.Messages {
		if err := validateMessageAttachments(message); err != nil {
			return err
		}
	}
	if err := s.validateAttachmentsForModel(s.resolveModel(conv.Model, s.loadSettings().DefaultModel), attachmentsForConversationTurn(conv.Messages)); err != nil {
		return err
	}
	return nil
}

func (s *Service) validateModelName(name string) resultFailure {
	if core.Trim(name) == "" {
		return core.E("chat.selectModel", "model is required", nil)
	}
	if len(s.discoverModels()) == 0 {
		return nil
	}
	if _, ok := s.findModel(name); ok {
		return nil
	}
	return core.E("chat.selectModel", "model is not available: "+name, nil)
}

func (s *Service) validateOptionalModelName(name string) resultFailure {
	if core.Trim(name) == "" {
		return nil
	}
	if len(s.discoverModels()) == 0 || equalFold(core.Trim(name), "default") {
		return nil
	}
	if _, ok := s.findModel(name); ok {
		return nil
	}
	return core.E("chat.model", "model is not available: "+name, nil)
}

func (s *Service) findModel(name string) (ModelEntry, bool) {
	for _, model := range s.discoverModels() {
		if equalFold(model.Name, core.Trim(name)) {
			return model, true
		}
	}
	return ModelEntry{}, false
}

func (s *Service) validateAttachmentsForModel(modelName string, attachments []ImageAttachment) resultFailure {
	if len(attachments) == 0 {
		return nil
	}
	model, ok := s.findModel(modelName)
	if !ok {
		return core.E("chat.send", "image attachments require a discovered vision-capable model", nil)
	}
	if !model.SupportsVision {
		return core.E("chat.send", "selected model does not support image input: "+model.Name, nil)
	}
	return nil
}

func validateMessageAttachments(message ChatMessage) resultFailure {
	for _, attachment := range message.Attachments {
		if err := validateImageAttachment(attachment); err != nil {
			return err
		}
	}
	return nil
}

func attachmentsForConversationTurn(messages []ChatMessage) []ImageAttachment {
	if len(messages) == 0 {
		return nil
	}
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role != "user" {
			continue
		}
		return messages[index].Attachments
	}
	return nil
}

func discoverModelsOnDisk(root string) []ModelEntry {
	if core.Trim(root) == "" {
		return nil
	}
	var results []ModelEntry
	entries := core.ReadDir(core.DirFS(root), ".")
	if !entries.OK {
		return nil
	}
	for _, entry := range entries.Value.([]core.FsDirEntry) {
		if !entry.IsDir() {
			continue
		}
		modelPath := core.PathJoin(root, entry.Name())
		config, ok := readModelConfig(modelPath)
		if !ok {
			continue
		}
		name := core.PathBase(modelPath)
		size := directorySize(modelPath)
		results = append(results, ModelEntry{
			Name:           name,
			Size:           size,
			Architecture:   core.Lower(config.ModelType),
			QuantBits:      coalesceQuantBits(config.Quantization.Bits, quantBitsFromName(name)),
			SizeBytes:      size,
			Backend:        "local",
			SupportsVision: architectureSupportsVision(config.ModelType),
		})
	}
	return results
}

func readModelConfig(modelPath string) (modelConfig, bool) {
	var config modelConfig
	body, err := coreReadFile(core.PathJoin(modelPath, "config.json"))
	if err != nil {
		return config, false
	}
	if result := core.JSONUnmarshal(body, &config); !result.OK {
		return config, false
	}
	if core.Trim(config.ModelType) == "" {
		config.ModelType = core.PathBase(modelPath)
	}
	return config, true
}

func validateImageAttachment(attachment ImageAttachment) resultFailure {
	if core.Trim(attachment.Filename) == "" {
		return core.E("chat.attachImage", "attachment filename is required", nil)
	}
	mimeType := core.Lower(core.Trim(attachment.MimeType))
	if _, ok := supportedImageMimeTypes[mimeType]; !ok {
		return core.E("chat.attachImage", "unsupported image format: expected PNG, JPEG, WebP, or GIF", nil)
	}
	if core.Trim(attachment.Data) == "" {
		return core.E("chat.attachImage", "attachment data is required", nil)
	}
	return nil
}

func imageAttachmentFromFile(rawPath string) (ImageAttachment, resultFailure) {
	path, err := validatedImageFilePath(rawPath)
	if err != nil {
		return ImageAttachment{}, err
	}
	content, err := coreReadFile(path)
	if err != nil {
		return ImageAttachment{}, core.E("chat.attachImageFile", "failed to read image file", err)
	}
	data := content
	mimeType, err := detectImageMimeType(path, data)
	if err != nil {
		return ImageAttachment{}, err
	}
	width, height := imageDimensionsFromBytes(data)
	attachment := ImageAttachment{
		Filename: core.PathBase(path),
		MimeType: mimeType,
		Data:     base64.StdEncoding.EncodeToString(data),
		Width:    width,
		Height:   height,
	}
	if err := validateImageAttachment(attachment); err != nil {
		return ImageAttachment{}, err
	}
	return attachment, nil
}

func validatedImageFilePath(rawPath string) (string, resultFailure) {
	trimmed := core.Trim(rawPath)
	if trimmed == "" {
		return "", core.E("chat.attachImageFile", "path is required", nil)
	}
	if core.Contains(trimmed, "\x00") {
		return "", core.E("chat.attachImageFile", "path contains a null byte", nil)
	}
	cleaned := core.CleanPath(trimmed, string(core.PathSeparator))
	if !core.PathIsAbs(cleaned) {
		return "", core.E("chat.attachImageFile", "path must be absolute", nil)
	}
	return cleaned, nil
}

func detectImageMimeType(path string, data []byte) (string, resultFailure) {
	mimeType := core.Lower(core.Trim(http.DetectContentType(data)))
	if _, ok := supportedImageMimeTypes[mimeType]; ok {
		return mimeType, nil
	}

	switch core.Lower(core.PathExt(path)) {
	case ".png":
		return "image/png", nil
	case ".jpg", ".jpeg":
		return "image/jpeg", nil
	case ".webp":
		return "image/webp", nil
	case ".gif":
		return "image/gif", nil
	default:
		return "", core.E("chat.attachImageFile", "unsupported image format: expected PNG, JPEG, WebP, or GIF", nil)
	}
}

func imageDimensionsFromBytes(data []byte) (int, int) {
	config, _, err := image.DecodeConfig(core.NewBuffer(data))
	if err != nil {
		return 0, 0
	}
	return config.Width, config.Height
}

func architectureSupportsVision(architecture string) bool {
	lower := core.Lower(core.Trim(architecture))
	return core.HasPrefix(lower, "gemma3") || core.HasPrefix(lower, "gemma4")
}

func coalesceQuantBits(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func quantBitsFromName(name string) int {
	lower := core.Lower(name)
	switch {
	case core.Contains(lower, "q4"), core.Contains(lower, "4bit"):
		return 4
	case core.Contains(lower, "q8"), core.Contains(lower, "8bit"):
		return 8
	default:
		return 0
	}
}

func directorySize(root string) int64 {
	var total int64
	// core.PathWalk returns core.Result since v0.11.0; a failed walk still
	// reports whatever summed before the failure — same semantics as before.
	core.PathWalk(root, func(path string, info core.FsFileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if core.HasSuffix(info.Name(), ".safetensors") || core.HasSuffix(info.Name(), ".gguf") || core.HasSuffix(info.Name(), ".bin") {
			total += info.Size()
		}
		return nil
	})
	return total
}
