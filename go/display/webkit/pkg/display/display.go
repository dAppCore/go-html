package display

import (
	"context"
	"math"
	"net/url"
	"runtime"
	"sync"

	core "dappco.re/go"
	"dappco.re/go/config"

	"dappco.re/go/render/display/webkit/pkg/chat"
	"dappco.re/go/render/display/webkit/pkg/clipboard"
	"dappco.re/go/render/display/webkit/pkg/container"
	"dappco.re/go/render/display/webkit/pkg/contextmenu"
	"dappco.re/go/render/display/webkit/pkg/deno"
	"dappco.re/go/render/display/webkit/pkg/dialog"
	"dappco.re/go/render/display/webkit/pkg/dock"
	"dappco.re/go/render/display/webkit/pkg/environment"
	"dappco.re/go/render/display/webkit/pkg/events"
	"dappco.re/go/render/display/webkit/pkg/internal/coreutil"
	"dappco.re/go/render/display/webkit/pkg/keybinding"
	"dappco.re/go/render/display/webkit/pkg/lifecycle"
	"dappco.re/go/render/display/webkit/pkg/menu"
	"dappco.re/go/render/display/webkit/pkg/notification"
	"dappco.re/go/render/display/webkit/pkg/screen"
	"dappco.re/go/render/display/webkit/pkg/systray"
	"dappco.re/go/render/display/webkit/pkg/webview"
	"dappco.re/go/render/display/webkit/pkg/window"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type Options struct{}

func failedAction(method, action string) resultFailure {
	return core.E(method, action+" action failed", nil)
}

var (
	maxInt = int(^uint(0) >> 1)
	minInt = -int(^uint(0)>>1) - 1
)

// WindowInfo is an alias for window.WindowInfo (backward compatibility).
type WindowInfo = window.WindowInfo

// Service orchestrates window, systray, and menu sub-services via IPC.
// Bridges IPC actions to WebSocket events for TypeScript apps.
type Service struct {
	*core.ServiceRuntime[Options]
	wailsApp        *application.App
	app             App
	configData      map[string]map[string]any
	configFile      *config.Config // config instance for file persistence
	mode            container.AppMode
	events          *WSEventManager
	p2pBridgeCancel context.CancelFunc
	schemeHandlers  map[string]SchemeHandler
	manifestCache   map[string]*loadedManifest
	manifestMu      sync.Mutex
	storage         *StorageRegistry
	background      *BackgroundRegistry
	sidecar         *deno.Manager
}

// New returns a display Service with empty config sections.
// s, _ := display.New(); s.loadConfigFrom("/path/to/config.yaml")
func New() (*Service, resultFailure) {
	return &Service{
		configData: map[string]map[string]any{
			"window":  {},
			"systray": {},
			"menu":    {},
		},
		mode:           container.DetectMode(),
		schemeHandlers: make(map[string]SchemeHandler),
		manifestCache:  make(map[string]*loadedManifest),
		storage:        NewStorageRegistry(),
		background:     NewBackgroundRegistry(),
	}, nil
}

// Register binds the display service to a Core instance.
// core.WithService(display.Register(app))      // production (Wails app)
// core.WithService(display.Register(nil))      // tests (no Wails runtime)
func Register(wailsApp *application.App) func(*core.Core) core.Result {
	return func(c *core.Core) core.Result {
		s, err := New()
		if err != nil {
			return core.Result{Value: err, OK: false}
		}
		s.ServiceRuntime = core.NewServiceRuntime[Options](c, Options{})
		s.wailsApp = wailsApp
		if result := c.RegisterService("display", s); !result.OK {
			return result
		}
		if !c.Service("deno").OK {
			if result := c.RegisterService("deno", s.ensureSidecar()); !result.OK {
				return result
			}
		}
		if !c.Service("tim").OK {
			if result := c.RegisterService("tim", container.NewService(c, container.OptionsFromEnv())); !result.OK {
				return result
			}
		}
		return core.Result{OK: true}
	}
}

// OnStartup loads config and registers handlers before sub-services start.
// Config handlers are registered first — sub-services query them during their own OnStartup.
func (s *Service) OnStartup(_ context.Context) core.Result {
	s.loadConfig()
	s.mode = container.DetectMode()

	// Register config query handler — available NOW for sub-services
	s.Core().RegisterQuery(s.handleConfigQuery)

	// Register config save actions
	s.Core().Action("display.saveWindowConfig", func(_ context.Context, opts core.Options) core.Result {
		t, _ := opts.Get("task").Value.(window.TaskSaveConfig)
		s.configData["window"] = t.Config
		if err := s.persistSection("window", t.Config); err != nil {
			return core.Result{Value: err, OK: false}
		}
		return core.Result{OK: true}
	})
	s.Core().Action("display.saveSystrayConfig", func(_ context.Context, opts core.Options) core.Result {
		t, _ := opts.Get("task").Value.(systray.TaskSaveConfig)
		s.configData["systray"] = t.Config
		if err := s.persistSection("systray", t.Config); err != nil {
			return core.Result{Value: err, OK: false}
		}
		return core.Result{OK: true}
	})
	s.Core().Action("display.saveMenuConfig", func(_ context.Context, opts core.Options) core.Result {
		t, _ := opts.Get("task").Value.(menu.TaskSaveConfig)
		s.configData["menu"] = t.Config
		if err := s.persistSection("menu", t.Config); err != nil {
			return core.Result{Value: err, OK: false}
		}
		return core.Result{OK: true}
	})
	s.Core().Action("display.buildPreload", func(_ context.Context, opts core.Options) core.Result {
		script, err := s.BuildPreloadScript(opts.String("url"))
		return core.Result{}.New(script, err)
	})
	s.Core().Action("display.resolve_scheme", func(ctx context.Context, opts core.Options) core.Result {
		return s.ResolveScheme(ctx, opts.String("url"))
	})
	s.Core().Action("display.storage.set", func(_ context.Context, opts core.Options) core.Result {
		origin := opts.String("origin")
		bucket := opts.String("bucket")
		key := opts.String("key")
		value := opts.String("value")
		if s.storage == nil {
			return core.Result{Value: core.E("display.storage.set", "storage registry unavailable", nil), OK: false}
		}
		if !s.storage.Set(origin, bucket, key, value) {
			return core.Result{Value: core.E("display.storage.set", "invalid storage entry", nil), OK: false}
		}
		return core.Result{Value: map[string]string{"origin": origin, "bucket": bucket, "key": key}, OK: true}
	})
	s.Core().Action("display.storage.delete", func(_ context.Context, opts core.Options) core.Result {
		origin := opts.String("origin")
		bucket := opts.String("bucket")
		key := opts.String("key")
		if s.storage == nil {
			return core.Result{Value: core.E("display.storage.delete", "storage registry unavailable", nil), OK: false}
		}
		if !s.storage.Delete(origin, bucket, key) {
			return core.Result{Value: core.E("display.storage.delete", "invalid storage entry", nil), OK: false}
		}
		return core.Result{Value: map[string]string{"origin": origin, "bucket": bucket, "key": key}, OK: true}
	})
	s.Core().Action("display.storage.search", func(_ context.Context, opts core.Options) core.Result {
		return core.Result{Value: s.searchAllStorage(opts.String("q")), OK: true}
	})
	s.Core().RegisterQuery(func(_ *core.Core, q core.Query) core.Result {
		switch typed := q.(type) {
		case QueryStoreRoute:
			return s.handleStoreSearch(context.Background(), url.Values{"q": []string{typed.Query}})
		default:
			return core.Result{}
		}
	})
	s.Core().Action("display.models.state", func(_ context.Context, _ core.Options) core.Result {
		return core.Result{Value: s.modelState(), OK: true}
	})
	s.Core().Action("display.network.state", func(_ context.Context, _ core.Options) core.Result {
		return core.Result{Value: s.networkState(), OK: true}
	})
	s.registerBackgroundActions()
	s.registerMarketplaceActions()
	s.registerSidecarActions()
	s.registerDefaultSchemes()

	// Initialise Wails wrappers if app is available (nil in tests)
	if s.wailsApp != nil {
		s.app = newWailsApp(s.wailsApp)
		if s.events == nil {
			s.events = NewWSEventManager()
		}
	}

	s.attachP2PBridge()

	return core.Result{OK: true}
}

func (s *Service) OnShutdown(ctx context.Context) core.Result {
	events := s.events
	s.events = nil
	if s.p2pBridgeCancel != nil {
		s.p2pBridgeCancel()
		s.p2pBridgeCancel = nil
	}
	if events != nil {
		events.Close()
	}
	var shutdownErr resultFailure
	if s.storage != nil {
		if err := s.storage.Close(); err != nil {
			shutdownErr = err
		}
	}
	if s.sidecar != nil {
		_, err := s.sidecar.Stop(ctx)
		if err != nil {
			if shutdownErr != nil {
				shutdownErr = core.ErrorJoin(shutdownErr, err)
			} else {
				shutdownErr = err
			}
		}
	}
	return core.Result{}.New(nil, shutdownErr)
}

// HandleIPCEvents bridges IPC actions from sub-services to WebSocket events for TS apps.
func (s *Service) HandleIPCEvents(c *core.Core, msg core.Message) core.Result {
	s.forwardIPCToSidecar(msg)
	switch m := msg.(type) {
	case core.ActionServiceStartup:
		// All services have completed OnStartup — safe to call sub-services
		s.buildMenu()
		s.setupTray()
	case window.ActionWindowOpened:
		if s.events != nil {
			s.events.Emit(Event{Type: EventWindowCreate, Window: m.Name,
				Data: map[string]any{"name": m.Name}})
		}
	case window.ActionWindowClosed:
		if s.events != nil {
			s.events.Emit(Event{Type: EventWindowClose, Window: m.Name,
				Data: map[string]any{"name": m.Name}})
		}
	case window.ActionWindowMoved:
		if s.events != nil {
			s.events.Emit(Event{Type: EventWindowMove, Window: m.Name,
				Data: map[string]any{"x": m.X, "y": m.Y}})
		}
	case window.ActionWindowResized:
		if s.events != nil {
			s.events.Emit(Event{Type: EventWindowResize, Window: m.Name,
				Data: map[string]any{"w": m.Width, "h": m.Height}})
		}
	case window.ActionWindowFocused:
		if s.events != nil {
			s.events.Emit(Event{Type: EventWindowFocus, Window: m.Name})
		}
	case window.ActionWindowBlurred:
		if s.events != nil {
			s.events.Emit(Event{Type: EventWindowBlur, Window: m.Name})
		}
	case systray.ActionTrayClicked:
		if s.events != nil {
			s.events.Emit(Event{Type: EventTrayClick})
		}
	case systray.ActionTrayMenuItemClicked:
		if s.events != nil {
			s.events.Emit(Event{Type: EventTrayMenuItemClick,
				Data: map[string]any{"actionId": m.ActionID}})
		}
		s.handleTrayAction(m.ActionID)
	case environment.ActionThemeChanged:
		if s.events != nil {
			theme := "light"
			if m.IsDark {
				theme = "dark"
			}
			s.events.Emit(Event{Type: EventThemeChange,
				Data: map[string]any{"isDark": m.IsDark, "theme": theme}})
		}
	case notification.ActionNotificationClicked:
		if s.events != nil {
			s.events.Emit(Event{Type: EventNotificationClick,
				Data: map[string]any{"id": m.ID}})
		}
	case screen.ActionScreensChanged:
		if s.events != nil {
			s.events.Emit(Event{Type: EventScreenChange,
				Data: map[string]any{"screens": m.Screens}})
		}
	case keybinding.ActionTriggered:
		if s.events != nil {
			s.events.Emit(Event{Type: EventKeybindingTriggered,
				Data: map[string]any{"accelerator": m.Accelerator}})
		}
	case window.ActionFilesDropped:
		if s.events != nil {
			s.events.Emit(Event{Type: EventWindowFileDrop, Window: m.Name,
				Data: map[string]any{"paths": m.Paths, "targetId": m.TargetID}})
		}
	case dock.ActionVisibilityChanged:
		if s.events != nil {
			s.events.Emit(Event{Type: EventDockVisibility,
				Data: map[string]any{"visible": m.Visible}})
		}
	case lifecycle.ActionApplicationStarted:
		if s.events != nil {
			s.events.Emit(Event{Type: EventAppStarted})
		}
	case lifecycle.ActionOpenedWithFile:
		if s.events != nil {
			s.events.Emit(Event{Type: EventAppOpenedWithFile,
				Data: map[string]any{core.Concat("pa", "th"): m.Path}})
		}
	case lifecycle.ActionWillTerminate:
		if s.events != nil {
			s.events.Emit(Event{Type: EventAppWillTerminate})
		}
	case lifecycle.ActionDidBecomeActive:
		if s.events != nil {
			s.events.Emit(Event{Type: EventAppActive})
		}
	case lifecycle.ActionDidResignActive:
		if s.events != nil {
			s.events.Emit(Event{Type: EventAppInactive})
		}
	case lifecycle.ActionPowerStatusChanged:
		if s.events != nil {
			s.events.Emit(Event{Type: EventSystemPowerChange})
		}
	case lifecycle.ActionSystemSuspend:
		if s.events != nil {
			s.events.Emit(Event{Type: EventSystemSuspend})
		}
	case lifecycle.ActionSystemResume:
		if s.events != nil {
			s.events.Emit(Event{Type: EventSystemResume})
		}
	case contextmenu.ActionItemClicked:
		if s.events != nil {
			s.events.Emit(Event{Type: EventContextMenuClick,
				Data: map[string]any{
					"menuName": m.MenuName,
					"actionId": m.ActionID,
					"data":     m.Data,
				}})
		}
	case webview.ActionConsoleMessage:
		if s.events != nil {
			s.events.Emit(Event{Type: EventWebviewConsole, Window: m.Window,
				Data: map[string]any{"message": m.Message}})
		}
	case webview.ActionException:
		if s.events != nil {
			s.events.Emit(Event{Type: EventWebviewException, Window: m.Window,
				Data: map[string]any{"exception": m.Exception}})
		}
	case ActionIDECommand:
		if s.events != nil {
			s.events.Emit(Event{Type: EventIDECommand,
				Data: map[string]any{"command": m.Command}})
		}
	case events.ActionEventFired:
		if s.events != nil {
			s.events.Emit(Event{Type: EventCustomEvent,
				Data: map[string]any{"name": m.Event.Name, "data": m.Event.Data}})
		}
	case dock.ActionProgressChanged:
		if s.events != nil {
			s.events.Emit(Event{Type: EventDockProgress,
				Data: map[string]any{"progress": m.Progress}})
		}
	case dock.ActionBounceStarted:
		if s.events != nil {
			s.events.Emit(Event{Type: EventDockBounce,
				Data: map[string]any{"requestId": m.RequestID, "bounceType": m.BounceType}})
		}
	case notification.ActionNotificationActionTriggered:
		if s.events != nil {
			s.events.Emit(Event{Type: EventNotificationAction,
				Data: map[string]any{"notificationId": m.NotificationID, "actionId": m.ActionID}})
		}
	case notification.ActionNotificationDismissed:
		if s.events != nil {
			s.events.Emit(Event{Type: EventNotificationDismiss,
				Data: map[string]any{"id": m.ID}})
		}
	case chat.ActionConversationCreated:
		if s.events != nil {
			s.events.Emit(Event{Type: EventChatConversation,
				Data: map[string]any{"action": "created", "conversation": m.Conversation}})
		}
	case chat.ActionConversationUpdated:
		if s.events != nil {
			s.events.Emit(Event{Type: EventChatConversation,
				Data: map[string]any{"action": "updated", "conversation": m.Conversation}})
		}
	case chat.ActionConversationDeleted:
		if s.events != nil {
			s.events.Emit(Event{Type: EventChatConversation,
				Data: map[string]any{"action": "deleted", "conversationId": m.ConversationID}})
		}
	case chat.ActionConversationCleared:
		if s.events != nil {
			s.events.Emit(Event{Type: EventChatConversation,
				Data: map[string]any{"action": "cleared", "conversationId": m.ConversationID}})
		}
	case chat.ActionMessageAdded:
		if s.events != nil {
			s.events.Emit(Event{Type: EventChatMessage,
				Data: map[string]any{"conversationId": m.ConversationID, "message": m.Message}})
		}
	case chat.ActionStreamStarted:
		if s.events != nil {
			s.events.Emit(Event{Type: EventChatMessage,
				Data: map[string]any{
					"conversationId": m.ConversationID,
					"messageId":      m.MessageID,
					"streamId":       m.StreamID,
					"state":          "started",
				}})
		}
	case chat.ActionTokenAppended:
		if s.events != nil {
			s.events.Emit(Event{Type: EventChatToken,
				Data: map[string]any{
					"conversationId": m.ConversationID,
					"messageId":      m.MessageID,
					"content":        m.Content,
				}})
		}
	case chat.ActionStreamFinished:
		if s.events != nil {
			s.events.Emit(Event{Type: EventChatMessage,
				Data: map[string]any{
					"conversationId": m.ConversationID,
					"messageId":      m.MessageID,
					"state":          "finished",
					"finishReason":   m.FinishReason,
				}})
		}
	case chat.ActionThinkingStarted:
		if s.events != nil {
			s.events.Emit(Event{Type: EventChatThinkingStart,
				Data: map[string]any{
					"conversationId": m.ConversationID,
					"messageId":      m.MessageID,
					"startedAt":      m.StartedAt,
				}})
		}
	case chat.ActionThinkingAppended:
		if s.events != nil {
			s.events.Emit(Event{Type: EventChatThinkingAppend,
				Data: map[string]any{
					"conversationId": m.ConversationID,
					"messageId":      m.MessageID,
					"content":        m.Content,
				}})
		}
	case chat.ActionThinkingEnded:
		if s.events != nil {
			s.events.Emit(Event{Type: EventChatThinkingEnd,
				Data: map[string]any{
					"conversationId": m.ConversationID,
					"messageId":      m.MessageID,
					"durationMs":     m.DurationMS,
				}})
		}
	case chat.ActionToolCallStarted:
		if s.events != nil {
			s.events.Emit(Event{Type: EventChatToolCall,
				Data: map[string]any{
					"conversationId": m.ConversationID,
					"messageId":      m.MessageID,
					"call":           m.Call,
				}})
		}
	case chat.ActionToolResultReady:
		if s.events != nil {
			s.events.Emit(Event{Type: EventChatToolResult,
				Data: map[string]any{
					"conversationId": m.ConversationID,
					"messageId":      m.MessageID,
					"result":         m.Result,
				}})
		}
	case chat.ActionImageQueued:
		if s.events != nil {
			s.events.Emit(Event{Type: EventChatImageQueued,
				Data: map[string]any{
					"conversationId": m.ConversationID,
					"attachment":     m.Attachment,
				}})
		}
	}
	return core.Result{OK: true}
}

// WSMessage represents a command received from a WebSocket client.
type WSMessage struct {
	Action string         `json:"action"`
	Data   map[string]any `json:"data,omitempty"`
}

// requireStringField extracts a string field from WebSocket data and fails when it is missing.
func requireStringField(data map[string]any, key string) (string, resultFailure) {
	v, _ := data[key].(string)
	if v == "" {
		return "", core.E("display.requireStringField", "missing required field \""+key+"\"", nil)
	}
	return v, nil
}

// wsRequire is kept for backward compatibility inside the display package.
func wsRequire(data map[string]any, key string) (string, resultFailure) {
	return requireStringField(data, key)
}

func requireFloatField(data map[string]any, key string) (float64, resultFailure) {
	value, ok := data[key]
	if !ok || value == nil {
		return 0, core.E("display.handleWSMessage", "missing required field \""+key+"\"", nil)
	}

	switch v := value.(type) {
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, core.E("display.handleWSMessage", "invalid required field \""+key+"\"", nil)
		}
		return v, nil
	case float32:
		f := float64(v)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, core.E("display.handleWSMessage", "invalid required field \""+key+"\"", nil)
		}
		return f, nil
	case int:
		return float64(v), nil
	case int8:
		return float64(v), nil
	case int16:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint8:
		return float64(v), nil
	case uint16:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	default:
		return 0, core.E("display.handleWSMessage", "invalid required field \""+key+"\"", nil)
	}
}

func intFromFloatField(value float64, key string) (int, resultFailure) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, core.E("display.handleWSMessage", "invalid required field \""+key+"\"", nil)
	}
	if math.Trunc(value) != value {
		return 0, core.E("display.handleWSMessage", "invalid required field \""+key+"\"", nil)
	}
	if value < float64(minInt) || value > float64(maxInt-1) {
		return 0, core.E("display.handleWSMessage", "invalid required field \""+key+"\"", nil)
	}
	return int(value), nil
}

func requireIntField(data map[string]any, key string) (int, resultFailure) {
	value, ok := data[key]
	if !ok || value == nil {
		return 0, core.E("display.handleWSMessage", "missing required field \""+key+"\"", nil)
	}

	switch v := value.(type) {
	case int:
		return v, nil
	case int8:
		return int(v), nil
	case int16:
		return int(v), nil
	case int32:
		return int(v), nil
	case int64:
		if v < int64(minInt) || v > int64(maxInt) {
			return 0, core.E("display.handleWSMessage", "invalid required field \""+key+"\"", nil)
		}
		return int(v), nil
	case uint:
		if uint64(v) > uint64(maxInt) {
			return 0, core.E("display.handleWSMessage", "invalid required field \""+key+"\"", nil)
		}
		return int(v), nil
	case uint8:
		return int(v), nil
	case uint16:
		return int(v), nil
	case uint32:
		if uint64(v) > uint64(maxInt) {
			return 0, core.E("display.handleWSMessage", "invalid required field \""+key+"\"", nil)
		}
		return int(v), nil
	case uint64:
		if v > uint64(maxInt) {
			return 0, core.E("display.handleWSMessage", "invalid required field \""+key+"\"", nil)
		}
		return int(v), nil
	case float64:
		return intFromFloatField(v, key)
	case float32:
		return intFromFloatField(float64(v), key)
	default:
		return 0, core.E("display.handleWSMessage", "invalid required field \""+key+"\"", nil)
	}
}

func optionsFromMap(data map[string]any) core.Options {
	items := make([]core.Option, 0, len(data))
	for key, value := range data {
		items = append(items, core.Option{Key: key, Value: value})
	}
	return core.NewOptions(items...)
}

// wsOptions is kept for backward compatibility inside the display package.
func wsOptions(data map[string]any) core.Options {
	return optionsFromMap(data)
}

// handleWSMessage bridges WebSocket commands to IPC calls.
func (s *Service) handleWSMessage(msg WSMessage) core.Result {
	ctx := context.Background()
	c := s.Core()

	switch msg.Action {
	case "chat:send":
		return c.Action("gui.chat.send").Run(ctx, wsOptions(msg.Data))
	case "chat:clear":
		return c.Action("gui.chat.clear").Run(ctx, wsOptions(msg.Data))
	case "chat:history":
		return c.Action("gui.chat.history").Run(ctx, wsOptions(msg.Data))
	case "chat:models":
		return c.Action("gui.chat.models").Run(ctx, wsOptions(msg.Data))
	case "chat:select-model":
		return c.Action("gui.chat.select_model").Run(ctx, wsOptions(msg.Data))
	case "chat:settings:save":
		return c.Action("gui.chat.settings.save").Run(ctx, wsOptions(msg.Data))
	case "chat:settings:defaults":
		return c.Action("gui.chat.settings.defaults").Run(ctx, wsOptions(msg.Data))
	case "chat:settings:load":
		return c.Action("gui.chat.settings.load").Run(ctx, wsOptions(msg.Data))
	case "chat:settings:reset":
		return c.Action("gui.chat.settings.reset").Run(ctx, wsOptions(msg.Data))
	case "chat:conversations:list":
		return c.Action("gui.chat.conversations.list").Run(ctx, wsOptions(msg.Data))
	case "chat:conversations:load":
		return c.Action("gui.chat.conversations.load").Run(ctx, wsOptions(msg.Data))
	case "chat:conversations:get":
		return c.Action("gui.chat.conversations.load").Run(ctx, wsOptions(msg.Data))
	case "chat:conversations:delete":
		return c.Action("gui.chat.conversations.delete").Run(ctx, wsOptions(msg.Data))
	case "chat:conversations:search":
		return c.Action("gui.chat.conversations.search").Run(ctx, wsOptions(msg.Data))
	case "chat:conversations:new":
		return c.Action("gui.chat.conversations.new").Run(ctx, wsOptions(msg.Data))
	case "chat:conversation:save":
		return c.Action("gui.chat.conversation.save").Run(ctx, wsOptions(msg.Data))
	case "chat:conversations:rename":
		return c.Action("gui.chat.conversations.rename").Run(ctx, wsOptions(msg.Data))
	case "chat:conversations:export":
		return c.Action("gui.chat.conversations.export").Run(ctx, wsOptions(msg.Data))
	case "chat:attach-image":
		return c.Action("gui.chat.attach_image").Run(ctx, wsOptions(msg.Data))
	case "chat:attach-image-file":
		return c.Action("gui.chat.attach_image_file").Run(ctx, wsOptions(msg.Data))
	case "chat:remove-image":
		return c.Action("gui.chat.remove_image").Run(ctx, wsOptions(msg.Data))
	case "chat:thinking:start":
		return c.Action("gui.chat.thinking.start").Run(ctx, wsOptions(msg.Data))
	case "chat:thinking:append":
		return c.Action("gui.chat.thinking.append").Run(ctx, wsOptions(msg.Data))
	case "chat:thinking:stop":
		return c.Action("gui.chat.thinking.stop").Run(ctx, wsOptions(msg.Data))
	case "chat:thinking:end":
		return c.Action("gui.chat.thinking.stop").Run(ctx, wsOptions(msg.Data))
	case "marketplace:list":
		return c.Action("display.marketplace.list").Run(ctx, wsOptions(msg.Data))
	case "marketplace:fetch":
		return c.Action("display.marketplace.fetch").Run(ctx, wsOptions(msg.Data))
	case "marketplace:verify":
		return c.Action("display.marketplace.verify").Run(ctx, wsOptions(msg.Data))
	case "marketplace:install":
		return c.Action("display.marketplace.install").Run(ctx, wsOptions(msg.Data))
	case "keybinding:add":
		accelerator, _ := msg.Data["accelerator"].(string)
		description, _ := msg.Data["description"].(string)
		return c.Action("keybinding.add").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: keybinding.TaskAdd{Accelerator: accelerator, Description: description}},
		))
	case "keybinding:remove":
		accelerator, _ := msg.Data["accelerator"].(string)
		return c.Action("keybinding.remove").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: keybinding.TaskRemove{Accelerator: accelerator}},
		))
	case "keybinding:list":
		return c.QUERY(keybinding.QueryList{})
	case "browser:open-url":
		url, _ := msg.Data["url"].(string)
		return c.Action("browser.open_url").Run(ctx, core.NewOptions(
			core.Option{Key: "url", Value: url},
		))
	case "browser:open-file":
		path, _ := msg.Data[core.Concat("pa", "th")].(string)
		return c.Action("browser.open_file").Run(ctx, core.NewOptions(
			core.Option{Key: core.Concat("pa", "th"), Value: path},
		))
	case "clipboard:read":
		return c.QUERY(clipboard.QueryText{})
	case "clipboard:write":
		text, _ := msg.Data["text"].(string)
		return c.Action("clipboard.set_text").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: clipboard.TaskSetText{Text: text}},
		))
	case "clipboard:clear":
		return c.Action("clipboard.clear").Run(ctx, core.NewOptions())
	case "clipboard:read-image":
		return c.QUERY(clipboard.QueryImage{})
	case "clipboard:write-image":
		data, _ := msg.Data["base64"].(string)
		return c.Action("clipboard.set_image").Run(ctx, core.NewOptions(
			core.Option{Key: "data", Value: data},
		))
	case "dialog:open-file":
		return c.Action("dialog.open_file").Run(ctx, wsOptions(msg.Data))
	case "dialog:save-file":
		return c.Action("dialog.save_file").Run(ctx, wsOptions(msg.Data))
	case "dialog:open-directory":
		return c.Action("dialog.open_directory").Run(ctx, wsOptions(msg.Data))
	case "dialog:confirm":
		return c.Action("dialog.question").Run(ctx, wsOptions(msg.Data))
	case "dialog:message":
		return c.Action("dialog.message").Run(ctx, wsOptions(msg.Data))
	case "dialog:prompt":
		return c.Action("dialog.prompt").Run(ctx, wsOptions(msg.Data))
	case "dock:show":
		return c.Action("dock.show_icon").Run(ctx, core.NewOptions())
	case "dock:hide":
		return c.Action("dock.hide_icon").Run(ctx, core.NewOptions())
	case "dock:badge":
		label, _ := msg.Data["label"].(string)
		return c.Action("dock.set_badge").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: dock.TaskSetBadge{Label: label}},
		))
	case "dock:badge-remove":
		return c.Action("dock.remove_badge").Run(ctx, core.NewOptions())
	case "dock:visible":
		return c.QUERY(dock.QueryVisible{})
	case "notification:show":
		return c.Action("notification.send").Run(ctx, wsOptions(msg.Data))
	case "notification:clear":
		id, _ := msg.Data["id"].(string)
		return c.Action("notification.clear").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: notification.TaskClear{ID: id}},
		))
	case "notification:permission-request":
		return c.Action("notification.request_permission").Run(ctx, core.NewOptions())
	case "notification:permission-check":
		return c.QUERY(notification.QueryPermission{})
	case "notification:with-actions":
		return c.Action("notification.send").Run(ctx, wsOptions(msg.Data))
	case "theme:get":
		return c.QUERY(environment.QueryTheme{})
	case "theme:system":
		return c.QUERY(environment.QueryInfo{})
	case "theme:set":
		theme, _ := msg.Data["theme"].(string)
		return c.Action("environment.set_theme").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: environment.TaskSetTheme{Theme: theme}},
		))
	case "contextmenu:add":
		name, _ := msg.Data["name"].(string)
		menuValue, ok := msg.Data["menu"]
		if !ok || menuValue == nil {
			return core.Result{Value: core.E("display.handleWSMessage", "missing required field \"menu\"", nil), OK: false}
		}
		marshalResult := core.JSONMarshal(menuValue)
		if !marshalResult.OK {
			if err, ok := marshalResult.Value.(resultFailure); ok {
				return core.Result{Value: core.E("display.handleWSMessage", "failed to marshal menu definition", err), OK: false}
			}
			return core.Result{Value: core.E("display.handleWSMessage", "failed to marshal menu definition", nil), OK: false}
		}
		var menuDef contextmenu.ContextMenuDef
		menuJSON, _ := marshalResult.Value.([]byte)
		unmarshalResult := core.JSONUnmarshal(menuJSON, &menuDef)
		if !unmarshalResult.OK {
			if err, ok := unmarshalResult.Value.(resultFailure); ok {
				return core.Result{Value: core.E("display.handleWSMessage", "failed to unmarshal menu definition", err), OK: false}
			}
			return core.Result{Value: core.E("display.handleWSMessage", "failed to unmarshal menu definition", nil), OK: false}
		}
		return c.Action("contextmenu.add").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: contextmenu.TaskAdd{Name: name, Menu: menuDef}},
		))
	case "contextmenu:remove":
		name, _ := msg.Data["name"].(string)
		return c.Action("contextmenu.remove").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: contextmenu.TaskRemove{Name: name}},
		))
	case "contextmenu:get":
		name, _ := msg.Data["name"].(string)
		return c.QUERY(contextmenu.QueryGet{Name: name})
	case "contextmenu:list":
		return c.QUERY(contextmenu.QueryList{})
	case "webview:eval":
		w, e := wsRequire(msg.Data, "window")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		script, _ := msg.Data["script"].(string)
		return c.Action("webview.evaluate").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: webview.TaskEvaluate{Window: w, Script: script}},
		))
	case "webview:click":
		w, e := wsRequire(msg.Data, "window")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		sel, e := wsRequire(msg.Data, "selector")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		return c.Action("webview.click").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: webview.TaskClick{Window: w, Selector: sel}},
		))
	case "webview:type":
		w, e := wsRequire(msg.Data, "window")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		sel, e := wsRequire(msg.Data, "selector")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		text, _ := msg.Data["text"].(string)
		return c.Action("webview.type").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: webview.TaskType{Window: w, Selector: sel, Text: text}},
		))
	case "webview:navigate":
		w, e := wsRequire(msg.Data, "window")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		url, e := wsRequire(msg.Data, "url")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		return c.Action("webview.navigate").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: webview.TaskNavigate{Window: w, URL: url}},
		))
	case "webview:screenshot":
		w, e := wsRequire(msg.Data, "window")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		return c.Action("webview.screenshot").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: webview.TaskScreenshot{Window: w}},
		))
	case "webview:scroll":
		w, e := wsRequire(msg.Data, "window")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		x, _ := msg.Data["x"].(float64)
		y, _ := msg.Data["y"].(float64)
		return c.Action("webview.scroll").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: webview.TaskScroll{Window: w, X: int(x), Y: int(y)}},
		))
	case "webview:hover":
		w, e := wsRequire(msg.Data, "window")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		sel, e := wsRequire(msg.Data, "selector")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		return c.Action("webview.hover").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: webview.TaskHover{Window: w, Selector: sel}},
		))
	case "webview:select":
		w, e := wsRequire(msg.Data, "window")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		sel, e := wsRequire(msg.Data, "selector")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		val, _ := msg.Data["value"].(string)
		return c.Action("webview.select").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: webview.TaskSelect{Window: w, Selector: sel, Value: val}},
		))
	case "webview:check":
		w, e := wsRequire(msg.Data, "window")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		sel, e := wsRequire(msg.Data, "selector")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		checked, _ := msg.Data["checked"].(bool)
		return c.Action("webview.check").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: webview.TaskCheck{Window: w, Selector: sel, Checked: checked}},
		))
	case "webview:upload":
		w, e := wsRequire(msg.Data, "window")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		sel, e := wsRequire(msg.Data, "selector")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		pathsRaw, _ := msg.Data["paths"].([]any)
		var paths []string
		for _, p := range pathsRaw {
			if ps, ok := p.(string); ok {
				paths = append(paths, ps)
			}
		}
		return c.Action("webview.upload_file").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: webview.TaskUploadFile{Window: w, Selector: sel, Paths: paths}},
		))
	case "webview:viewport":
		w, e := wsRequire(msg.Data, "window")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		width, _ := msg.Data["width"].(float64)
		height, _ := msg.Data["height"].(float64)
		return c.Action("webview.set_viewport").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: webview.TaskSetViewport{Window: w, Width: int(width), Height: int(height)}},
		))
	case "webview:clear-console":
		w, e := wsRequire(msg.Data, "window")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		return c.Action("webview.clear_console").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: webview.TaskClearConsole{Window: w}},
		))
	case "webview:console":
		w, e := wsRequire(msg.Data, "window")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		level, _ := msg.Data["level"].(string)
		limit := 100
		if l, ok := msg.Data["limit"].(float64); ok {
			limit = int(l)
		}
		return c.QUERY(webview.QueryConsole{Window: w, Level: level, Limit: limit})
	case "webview:query":
		w, e := wsRequire(msg.Data, "window")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		sel, e := wsRequire(msg.Data, "selector")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		return c.QUERY(webview.QuerySelector{Window: w, Selector: sel})
	case "webview:query-all":
		w, e := wsRequire(msg.Data, "window")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		sel, e := wsRequire(msg.Data, "selector")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		return c.QUERY(webview.QuerySelectorAll{Window: w, Selector: sel})
	case "webview:dom-tree":
		w, e := wsRequire(msg.Data, "window")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		sel, _ := msg.Data["selector"].(string) // selector optional for dom-tree (defaults to root)
		return c.QUERY(webview.QueryDOMTree{Window: w, Selector: sel})
	case "webview:url":
		w, e := wsRequire(msg.Data, "window")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		return c.QUERY(webview.QueryURL{Window: w})
	case "webview:title":
		w, e := wsRequire(msg.Data, "window")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		return c.QUERY(webview.QueryTitle{Window: w})
	case "webview:devtools-open":
		w, e := wsRequire(msg.Data, "window")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		return c.Action("webview.devtools_open").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: webview.TaskDevToolsOpen{Window: w}},
		))
	case "webview:devtools-close":
		w, e := wsRequire(msg.Data, "window")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		return c.Action("webview.devtools_close").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: webview.TaskDevToolsClose{Window: w}},
		))
	case "tray:show-message":
		title, _ := msg.Data["title"].(string)
		message, _ := msg.Data["message"].(string)
		return c.Action("systray.show_message").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: systray.TaskShowMessage{Title: title, Message: message}},
		))
	case "layout:beside-editor":
		name, e := wsRequire(msg.Data, "name")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		editor, _ := msg.Data["editor"].(string)
		side, _ := msg.Data["side"].(string)
		ratio, e := requireFloatField(msg.Data, "ratio")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		return c.Action("window.layout_beside_editor").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: window.TaskLayoutBesideEditor{
				Name: name, Editor: editor, Side: side, Ratio: ratio,
			}},
		))
	case "layout:suggest":
		screenID, _ := msg.Data["screen_id"].(string)
		windowCount, e := requireIntField(msg.Data, "window_count")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		return c.Action("window.layout_suggest").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: window.TaskLayoutSuggest{
				ScreenID: screenID, WindowCount: windowCount,
			}},
		))
	case "screen:find-space":
		screenID, _ := msg.Data["screen_id"].(string)
		width, e := requireIntField(msg.Data, "width")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		height, e := requireIntField(msg.Data, "height")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		padding, e := requireIntField(msg.Data, "padding")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		return c.Action("window.find_space").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: window.TaskScreenFindSpace{
				ScreenID: screenID, Width: width, Height: height, Padding: padding,
			}},
		))
	case "window:arrange-pair":
		primary, e := wsRequire(msg.Data, "primary")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		secondary, e := wsRequire(msg.Data, "secondary")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		screenID, _ := msg.Data["screen_id"].(string)
		ratio, e := requireFloatField(msg.Data, "ratio")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		return c.Action("window.arrange_pair").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: window.TaskWindowArrangePair{
				Primary: primary, Secondary: secondary, ScreenID: screenID, Ratio: ratio,
			}},
		))
	case "window:set-opacity":
		name, e := wsRequire(msg.Data, "name")
		if e != nil {
			return core.Result{Value: e, OK: false}
		}
		opacity, ok := msg.Data["opacity"].(float64)
		if !ok {
			return core.Result{Value: core.E("display.handleWSMessage", "missing required field \"opacity\"", nil), OK: false}
		}
		return c.Action("window.set_opacity").Run(ctx, core.NewOptions(
			core.Option{Key: "task", Value: window.TaskSetOpacity{Name: name, Opacity: opacity}},
		))
	default:
		return core.Result{Value: core.E("display.handleWSMessage", "unknown websocket action: "+msg.Action, nil), OK: false}
	}
}

// handleTrayAction processes tray menu item clicks.
func (s *Service) handleTrayAction(actionID string) {
	ctx := context.Background()
	c := s.Core()
	switch actionID {
	case "open-desktop":
		// Show all windows
		infos := s.ListWindowInfos()
		for _, info := range infos {
			coreutil.ObserveResult(c, "display.handleTrayAction", "window focus failed", c.Action("window.focus").Run(ctx, core.NewOptions(
				core.Option{Key: "task", Value: window.TaskFocus{Name: info.Name}},
			)))
		}
	case "close-desktop":
		// Hide all tracked windows so the tray action behaves like a real desktop "close" without quitting.
		infos := s.ListWindowInfos()
		for _, info := range infos {
			coreutil.ObserveResult(c, "display.handleTrayAction", "window visibility update failed", c.Action("window.set_visibility").Run(ctx, core.NewOptions(
				core.Option{Key: "task", Value: window.TaskSetVisibility{Name: info.Name, Visible: false}},
			)))
		}
	case "env-info":
		// Query environment info via IPC and show as dialog
		r := c.QUERY(environment.QueryInfo{})
		if r.OK {
			info, _ := r.Value.(environment.EnvironmentInfo)
			details := "OS: " + info.OS + "\nArch: " + info.Arch + "\nPlatform: " +
				info.Platform.Name + " " + info.Platform.Version
			coreutil.ObserveResult(c, "display.handleTrayAction", "environment dialog failed", c.Action("dialog.message").Run(ctx, core.NewOptions(
				core.Option{Key: "task", Value: dialog.TaskMessageDialog{
					Options: dialog.MessageDialogOptions{
						Type: dialog.DialogInfo, Title: "Environment",
						Message: details, Buttons: []string{"OK"},
					},
				}},
			)))
		}
	case "quit":
		if s.app != nil {
			s.app.Quit()
		}
	}
}

func guiConfigPath() string {
	home := core.Env("HOME")
	if home == "" {
		return core.JoinPath(".core", "gui", "config.yaml")
	}
	return core.JoinPath(home, ".core", "gui", "config.yaml")
}

func (s *Service) loadConfig() {
	if s.configFile != nil {
		return // Already loaded (e.g., via loadConfigFrom in tests)
	}
	s.loadConfigFrom(guiConfigPath())
}

func (s *Service) loadConfigFrom(path string) {
	configFile, ok := core.Cast[*config.Config](config.New(config.WithPath(path)))
	if !ok {
		// Non-critical — continue with empty configData
		return
	}
	s.configFile = configFile

	for _, section := range []string{"window", "systray", "menu"} {
		var data map[string]any
		if result := configFile.Get(section, &data); result.OK && data != nil {
			s.configData[section] = data
		}
	}
}

func (s *Service) handleConfigQuery(_ *core.Core, q core.Query) core.Result {
	switch q.(type) {
	case window.QueryConfig:
		return core.Result{Value: s.configData["window"], OK: true}
	case systray.QueryConfig:
		return core.Result{Value: s.configData["systray"], OK: true}
	case menu.QueryConfig:
		return core.Result{Value: s.configData["menu"], OK: true}
	case QueryAppMode:
		return core.Result{Value: s.mode, OK: true}
	case events.QueryServerInfo:
		if s.events == nil {
			return core.Result{Value: events.ServerInfo{}, OK: true}
		}
		return core.Result{Value: s.events.Info(), OK: true}
	default:
		return core.Result{}
	}
}

func (s *Service) persistSection(key string, value map[string]any) resultFailure {
	if s.configFile == nil {
		return nil
	}
	if result := s.configFile.Set(key, value); !result.OK {
		return coreResultError(result, "failed to save config section")
	}
	if result := s.configFile.Commit(); !result.OK {
		return coreResultError(result, "failed to commit config")
	}
	return nil
}

// --- Service accessors ---

// windowService returns the window service from Core, or nil if not registered.
func (s *Service) windowService() *window.Service {
	if s == nil || s.ServiceRuntime == nil {
		return nil
	}
	svc, ok := core.ServiceFor[*window.Service](s.Core(), "window")
	if !ok {
		return nil
	}
	return svc
}

// --- Window Management (delegates via IPC) ---

// OpenWindow creates a new window via IPC.
func (s *Service) OpenWindow(options ...window.WindowOption) resultFailure {
	spec, err := window.ApplyOptions(options...)
	if err != nil {
		return err
	}
	r := s.Core().Action("window.open").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskOpenWindow{Window: spec}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return e
		}
		return core.E("display.OpenWindow", "window.open action failed", nil)
	}
	return nil
}

// GetWindowInfo returns information about a window via IPC.
func (s *Service) GetWindowInfo(name string) (*window.WindowInfo, resultFailure) {
	r := s.Core().QUERY(window.QueryWindowByName{Name: name})
	if !r.OK {
		if err, ok := r.Value.(resultFailure); ok {
			return nil, err
		}
		return nil, failedAction("display.GetWindowInfo", "window.queryWindowByName")
	}
	if r.Value == nil {
		return nil, nil
	}
	info, ok := r.Value.(*window.WindowInfo)
	if !ok {
		return nil, core.E("display.GetWindowInfo", "unexpected result type", nil)
	}
	return info, nil
}

// ListWindowInfos returns information about all tracked windows via IPC.
func (s *Service) ListWindowInfos() []window.WindowInfo {
	r := s.Core().QUERY(window.QueryWindowList{})
	if !r.OK {
		return []window.WindowInfo{}
	}
	list, _ := r.Value.([]window.WindowInfo)
	if list == nil {
		return []window.WindowInfo{}
	}
	return list
}

// SetWindowPosition moves a window via IPC.
func (s *Service) SetWindowPosition(name string, x, y int) resultFailure {
	r := s.Core().Action("window.set_position").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskSetPosition{Name: name, X: x, Y: y}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return e
		}
		return failedAction("display.SetWindowPosition", "window.set_position")
	}
	return nil
}

// SetWindowSize resizes a window via IPC.
func (s *Service) SetWindowSize(name string, width, height int) resultFailure {
	r := s.Core().Action("window.set_size").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskSetSize{Name: name, Width: width, Height: height}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return e
		}
		return failedAction("display.SetWindowSize", "window.set_size")
	}
	return nil
}

// SetWindowBounds sets both position and size of a window via IPC.
func (s *Service) SetWindowBounds(name string, x, y, width, height int) resultFailure {
	if err := s.SetWindowPosition(name, x, y); err != nil {
		return err
	}
	return s.SetWindowSize(name, width, height)
}

// MaximizeWindow maximizes a window via IPC.
func (s *Service) MaximizeWindow(name string) resultFailure {
	r := s.Core().Action("window.maximise").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskMaximise{Name: name}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return e
		}
		return failedAction("display.MaximizeWindow", "window.maximise")
	}
	return nil
}

// MinimizeWindow minimizes a window via IPC.
func (s *Service) MinimizeWindow(name string) resultFailure {
	r := s.Core().Action("window.minimise").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskMinimise{Name: name}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return e
		}
		return failedAction("display.MinimizeWindow", "window.minimise")
	}
	return nil
}

// FocusWindow brings a window to the front via IPC.
func (s *Service) FocusWindow(name string) resultFailure {
	r := s.Core().Action("window.focus").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskFocus{Name: name}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return e
		}
		return failedAction("display.FocusWindow", "window.focus")
	}
	return nil
}

// CloseWindow closes a window via IPC.
func (s *Service) CloseWindow(name string) resultFailure {
	r := s.Core().Action("window.close").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskCloseWindow{Name: name}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return e
		}
		return failedAction("display.CloseWindow", "window.close")
	}
	return nil
}

// RestoreWindow restores a maximized/minimized window.
func (s *Service) RestoreWindow(name string) resultFailure {
	r := s.Core().Action("window.restore").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskRestore{Name: name}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return e
		}
		return failedAction("display.RestoreWindow", "window.restore")
	}
	return nil
}

// SetWindowVisibility shows or hides a window.
func (s *Service) SetWindowVisibility(name string, visible bool) resultFailure {
	r := s.Core().Action("window.set_visibility").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskSetVisibility{Name: name, Visible: visible}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return e
		}
		return failedAction("display.SetWindowVisibility", "window.set_visibility")
	}
	return nil
}

// SetWindowAlwaysOnTop sets whether a window stays on top.
func (s *Service) SetWindowAlwaysOnTop(name string, alwaysOnTop bool) resultFailure {
	r := s.Core().Action("window.set_always_on_top").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskSetAlwaysOnTop{Name: name, AlwaysOnTop: alwaysOnTop}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return e
		}
		return failedAction("display.SetWindowAlwaysOnTop", "window.set_always_on_top")
	}
	return nil
}

// SetWindowTitle changes a window's title.
func (s *Service) SetWindowTitle(name string, title string) resultFailure {
	r := s.Core().Action("window.set_title").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskSetTitle{Name: name, Title: title}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return e
		}
		return failedAction("display.SetWindowTitle", "window.set_title")
	}
	return nil
}

// SetWindowFullscreen sets a window to fullscreen mode.
func (s *Service) SetWindowFullscreen(name string, fullscreen bool) resultFailure {
	r := s.Core().Action("window.fullscreen").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskFullscreen{Name: name, Fullscreen: fullscreen}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return e
		}
		return failedAction("display.SetWindowFullscreen", "window.fullscreen")
	}
	return nil
}

// SetWindowBackgroundColour sets the background colour of a window.
func (s *Service) SetWindowBackgroundColour(name string, r, g, b, a uint8) resultFailure {
	result := s.Core().Action("window.set_background_colour").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskSetBackgroundColour{
			Name: name, Red: r, Green: g, Blue: b, Alpha: a,
		}},
	))
	if !result.OK {
		if e, ok := result.Value.(resultFailure); ok {
			return e
		}
		return failedAction("display.SetWindowBackgroundColour", "window.set_background_colour")
	}
	return nil
}

// GetFocusedWindow returns the name of the currently focused window.
func (s *Service) GetFocusedWindow() string {
	infos := s.ListWindowInfos()
	for _, info := range infos {
		if info.Focused {
			return info.Name
		}
	}
	return ""
}

// GetWindowTitle returns the title of a window by name.
func (s *Service) GetWindowTitle(name string) (string, resultFailure) {
	info, err := s.GetWindowInfo(name)
	if err != nil {
		return "", err
	}
	if info == nil {
		return "", core.E("display.GetWindowTitle", "window not found: "+name, nil)
	}
	return info.Title, nil
}

// ResetWindowState clears saved window positions.
func (s *Service) ResetWindowState() resultFailure {
	ws := s.windowService()
	if ws != nil {
		ws.Manager().State().Clear()
	}
	return nil
}

// GetSavedWindowStates returns all saved window states.
func (s *Service) GetSavedWindowStates() map[string]window.WindowState {
	ws := s.windowService()
	if ws == nil {
		return map[string]window.WindowState{}
	}
	result := make(map[string]window.WindowState)
	for _, name := range ws.Manager().State().ListStates() {
		if state, ok := ws.Manager().State().GetState(name); ok {
			result[name] = state
		}
	}
	return result
}

// CreateWindowOptions specifies the initial state for a new named window.
// svc.CreateWindow(display.CreateWindowOptions{Name: "settings", URL: "/settings", Width: 800, Height: 600})
type CreateWindowOptions struct {
	Name   string `json:"name"`
	Title  string `json:"title,omitempty"`
	URL    string `json:"url,omitempty"`
	X      int    `json:"x,omitempty"`
	Y      int    `json:"y,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

func (s *Service) CreateWindow(options CreateWindowOptions) (*window.WindowInfo, resultFailure) {
	if options.Name == "" {
		return nil, core.E("display.CreateWindow", "window name is required", nil)
	}
	r := s.Core().Action("window.open").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskOpenWindow{
			Window: &window.Window{
				Name:   options.Name,
				Title:  options.Title,
				URL:    options.URL,
				Width:  options.Width,
				Height: options.Height,
				X:      options.X,
				Y:      options.Y,
			},
		}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return nil, e
		}
		return nil, core.E("display.CreateWindow", "window.open action failed", nil)
	}
	info, ok := r.Value.(window.WindowInfo)
	if !ok {
		return nil, core.E("display.CreateWindow", "unexpected result type", nil)
	}
	return &info, nil
}

// --- Layout delegation ---

// SaveLayout saves the current window arrangement as a named layout.
func (s *Service) SaveLayout(name string) resultFailure {
	r := s.Core().Action("window.save_layout").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskSaveLayout{Name: name}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return e
		}
		return failedAction("display.SaveLayout", "window.save_layout")
	}
	return nil
}

// RestoreLayout applies a saved layout.
func (s *Service) RestoreLayout(name string) resultFailure {
	r := s.Core().Action("window.restore_layout").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskRestoreLayout{Name: name}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return e
		}
		return failedAction("display.RestoreLayout", "window.restore_layout")
	}
	return nil
}

// ListLayouts returns all saved layout names with metadata.
func (s *Service) ListLayouts() []window.LayoutInfo {
	r := s.Core().QUERY(window.QueryLayoutList{})
	if !r.OK {
		return []window.LayoutInfo{}
	}
	layouts, _ := r.Value.([]window.LayoutInfo)
	if layouts == nil {
		return []window.LayoutInfo{}
	}
	return layouts
}

// DeleteLayout removes a saved layout by name.
func (s *Service) DeleteLayout(name string) resultFailure {
	r := s.Core().Action("window.delete_layout").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskDeleteLayout{Name: name}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return e
		}
		return failedAction("display.DeleteLayout", "window.delete_layout")
	}
	return nil
}

// GetLayout returns a specific layout by name.
func (s *Service) GetLayout(name string) *window.Layout {
	r := s.Core().QUERY(window.QueryLayoutGet{Name: name})
	if !r.OK {
		return nil
	}
	if r.Value == nil {
		return nil
	}
	layout, ok := r.Value.(*window.Layout)
	if !ok {
		return nil
	}
	return layout
}

// --- Tiling/snapping delegation ---

// TileWindows arranges windows in a tiled layout.
func (s *Service) TileWindows(mode window.TileMode, windowNames []string) resultFailure {
	r := s.Core().Action("window.tile_windows").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskTileWindows{Mode: mode.String(), Windows: windowNames}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return e
		}
		return failedAction("display.TileWindows", "window.tile_windows")
	}
	return nil
}

// SnapWindow snaps a window to a screen edge or corner.
func (s *Service) SnapWindow(name string, position window.SnapPosition) resultFailure {
	r := s.Core().Action("window.snap_window").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskSnapWindow{Name: name, Position: position.String()}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return e
		}
		return failedAction("display.SnapWindow", "window.snap_window")
	}
	return nil
}

// StackWindows arranges windows in a cascade pattern.
func (s *Service) StackWindows(windowNames []string, offsetX, offsetY int) resultFailure {
	r := s.Core().Action("window.stack_windows").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskStackWindows{Windows: windowNames, OffsetX: offsetX, OffsetY: offsetY}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return e
		}
		return failedAction("display.StackWindows", "window.stack_windows")
	}
	return nil
}

// ApplyWorkflowLayout applies a predefined layout for a specific workflow.
func (s *Service) ApplyWorkflowLayout(workflow window.WorkflowLayout) resultFailure {
	r := s.Core().Action("window.apply_workflow").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskApplyWorkflow{Workflow: workflow.String()}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return e
		}
		return failedAction("display.ApplyWorkflowLayout", "window.apply_workflow")
	}
	return nil
}

// LayoutBesideEditor places a window beside a detected editor window.
//
//	result, err := svc.LayoutBesideEditor("preview", "code", "right", 0.62)
func (s *Service) LayoutBesideEditor(name, editor, side string, ratio float64) (window.LayoutBesideEditorResult, resultFailure) {
	r := s.Core().Action("window.layout_beside_editor").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskLayoutBesideEditor{
			Name: name, Editor: editor, Side: side, Ratio: ratio,
		}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return window.LayoutBesideEditorResult{}, e
		}
		return window.LayoutBesideEditorResult{}, failedAction("display.LayoutBesideEditor", "window.layout_beside_editor")
	}
	result, ok := r.Value.(window.LayoutBesideEditorResult)
	if !ok {
		return window.LayoutBesideEditorResult{}, core.E("display.LayoutBesideEditor", "unexpected result type", nil)
	}
	return result, nil
}

// LayoutSuggest returns a layout recommendation for the current screen.
//
//	suggestion, err := svc.LayoutSuggest("", 2)
func (s *Service) LayoutSuggest(screenID string, windowCount int) (window.LayoutSuggestion, resultFailure) {
	r := s.Core().Action("window.layout_suggest").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskLayoutSuggest{
			ScreenID: screenID, WindowCount: windowCount,
		}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return window.LayoutSuggestion{}, e
		}
		return window.LayoutSuggestion{}, failedAction("display.LayoutSuggest", "window.layout_suggest")
	}
	result, ok := r.Value.(window.LayoutSuggestion)
	if !ok {
		return window.LayoutSuggestion{}, core.E("display.LayoutSuggest", "unexpected result type", nil)
	}
	return result, nil
}

// FindScreenSpace finds an unused rectangle for a new window.
//
//	space, err := svc.FindScreenSpace("", 800, 600, 24)
func (s *Service) FindScreenSpace(screenID string, width, height, padding int) (window.ScreenSpace, resultFailure) {
	r := s.Core().Action("window.find_space").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskScreenFindSpace{
			ScreenID: screenID, Width: width, Height: height, Padding: padding,
		}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return window.ScreenSpace{}, e
		}
		return window.ScreenSpace{}, failedAction("display.FindScreenSpace", "window.find_space")
	}
	result, ok := r.Value.(window.ScreenSpace)
	if !ok {
		return window.ScreenSpace{}, core.E("display.FindScreenSpace", "unexpected result type", nil)
	}
	return result, nil
}

// ArrangeWindowPair positions two windows in an optimal split.
//
//	arrangement, err := svc.ArrangeWindowPair("editor", "preview", "", 0.55)
func (s *Service) ArrangeWindowPair(primary, secondary, screenID string, ratio float64) (window.PairArrangement, resultFailure) {
	r := s.Core().Action("window.arrange_pair").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskWindowArrangePair{
			Primary: primary, Secondary: secondary, ScreenID: screenID, Ratio: ratio,
		}},
	))
	if !r.OK {
		if e, ok := r.Value.(resultFailure); ok {
			return window.PairArrangement{}, e
		}
		return window.PairArrangement{}, failedAction("display.ArrangeWindowPair", "window.arrange_pair")
	}
	result, ok := r.Value.(window.PairArrangement)
	if !ok {
		return window.PairArrangement{}, core.E("display.ArrangeWindowPair", "unexpected result type", nil)
	}
	return result, nil
}

// GetEventManager returns the event manager for WebSocket event subscriptions.
func (s *Service) GetEventManager() *WSEventManager {
	return s.events
}

// --- Menu (handlers stay in display, structure delegated via IPC) ---

func (s *Service) buildMenu() {
	developerItems := []menu.MenuItem{
		{Label: "New File", Accelerator: "CmdOrCtrl+N", OnClick: s.handleNewFile},
		{Label: "Open File...", Accelerator: "CmdOrCtrl+O", OnClick: s.handleOpenFile},
		{Label: "Save", Accelerator: "CmdOrCtrl+S", OnClick: s.handleSaveFile},
		{Type: "separator"},
		{Label: "Editor", OnClick: s.handleOpenEditor},
		{Label: "Terminal", OnClick: s.handleOpenTerminal},
		{Type: "separator"},
		{Label: "Run", Accelerator: "CmdOrCtrl+R", OnClick: s.handleRun},
		{Label: "Build", Accelerator: "CmdOrCtrl+B", OnClick: s.handleBuild},
	}
	if menuService, ok := core.ServiceFor[*menu.Service](s.Core(), "menu"); ok && menuService.ShowDevTools() {
		developerItems = append(developerItems,
			menu.MenuItem{Type: "separator"},
			menu.MenuItem{Label: "Open DevTools", OnClick: s.handleOpenDevTools},
			menu.MenuItem{Label: "Close DevTools", OnClick: s.handleCloseDevTools},
		)
	}

	items := []menu.MenuItem{
		{Role: pointerTo(menu.RoleAppMenu)},
		{Role: pointerTo(menu.RoleFileMenu)},
		{Role: pointerTo(menu.RoleViewMenu)},
		{Role: pointerTo(menu.RoleEditMenu)},
		{Label: "Workspace", Children: []menu.MenuItem{
			{Label: "New...", OnClick: s.handleNewWorkspace},
			{Label: "List", OnClick: s.handleListWorkspaces},
		}},
		{Label: "Developer", Children: developerItems},
		{Role: pointerTo(menu.RoleWindowMenu)},
		{Role: pointerTo(menu.RoleHelpMenu)},
	}

	// On non-macOS, remove the AppMenu role
	if runtime.GOOS != "darwin" {
		items = items[1:] // skip AppMenu
	}

	coreutil.ObserveResult(s.Core(), "display.setupApplicationMenu", "app menu setup failed", s.Core().Action("menu.set_app_menu").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: menu.TaskSetAppMenu{Items: items}},
	)))
}

// pointerTo returns a pointer to value.
//
//go:fix inline
func pointerTo[T any](value T) *T { return new(value) }

// --- Menu handler methods ---

func (s *Service) menuDevToolsWindow() string {
	if name := s.GetFocusedWindow(); name != "" {
		return name
	}
	infos := s.ListWindowInfos()
	if len(infos) == 1 {
		return infos[0].Name
	}
	return ""
}

func (s *Service) handleOpenDevTools() {
	windowName := s.menuDevToolsWindow()
	if windowName == "" {
		return
	}
	coreutil.ObserveResult(s.Core(), "display.handleOpenDevTools", "devtools open failed", s.Core().Action("webview.devtools_open").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: webview.TaskDevToolsOpen{Window: windowName}},
	)))
}

func (s *Service) handleCloseDevTools() {
	windowName := s.menuDevToolsWindow()
	if windowName == "" {
		return
	}
	coreutil.ObserveResult(s.Core(), "display.handleCloseDevTools", "devtools close failed", s.Core().Action("webview.devtools_close").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: webview.TaskDevToolsClose{Window: windowName}},
	)))
}

func (s *Service) handleNewWorkspace() {
	coreutil.ObserveResult(s.Core(), "display.handleNewWorkspace", "workspace window open failed", s.Core().Action("window.open").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskOpenWindow{
			Window: &window.Window{
				Name:   "workspace-new",
				Title:  "New Workspace",
				URL:    "/workspace/new",
				Width:  500,
				Height: 400,
			},
		}},
	)))
}

func (s *Service) handleListWorkspaces() {
	r := s.Core().Service("workspace")
	if !r.OK || r.Value == nil {
		return
	}
	lister, ok := r.Value.(interface{ ListWorkspaces() []string })
	if !ok {
		return
	}
	workspaces := lister.ListWorkspaces()
	if len(workspaces) == 0 {
		return
	}
}

func (s *Service) handleNewFile() {
	coreutil.ObserveResult(s.Core(), "display.handleNewFile", "editor window open failed", s.Core().Action("window.open").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskOpenWindow{
			Window: &window.Window{
				Name:   "editor",
				Title:  "New File - Editor",
				URL:    "/#/developer/editor?new=true",
				Width:  1200,
				Height: 800,
			},
		}},
	)))
}

func (s *Service) handleOpenFile() {
	r := s.Core().Action("dialog.open_file").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: dialog.TaskOpenFile{
			Options: dialog.OpenFileOptions{
				Title:         "Open File",
				AllowMultiple: false,
			},
		}},
	))
	if !r.OK {
		return
	}
	paths, ok := r.Value.([]string)
	if !ok || len(paths) == 0 {
		return
	}
	fileURL := "/#/developer/editor?file=" + url.QueryEscape(paths[0])
	coreutil.ObserveResult(s.Core(), "display.handleOpenFile", "editor file window open failed", s.Core().Action("window.open").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskOpenWindow{
			Window: &window.Window{
				Name:   "editor",
				Title:  paths[0] + " - Editor",
				URL:    fileURL,
				Width:  1200,
				Height: 800,
			},
		}},
	)))
}

func (s *Service) handleSaveFile() {
	coreutil.DispatchAction(s.Core(), "display.handleSaveFile", ActionIDECommand{Command: "save"})
}
func (s *Service) handleOpenEditor() {
	coreutil.ObserveResult(s.Core(), "display.handleOpenEditor", "editor window open failed", s.Core().Action("window.open").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskOpenWindow{
			Window: &window.Window{
				Name:   "editor",
				Title:  "Editor",
				URL:    "/#/developer/editor",
				Width:  1200,
				Height: 800,
			},
		}},
	)))
}

func (s *Service) handleOpenTerminal() {
	coreutil.ObserveResult(s.Core(), "display.handleOpenTerminal", "terminal window open failed", s.Core().Action("window.open").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: window.TaskOpenWindow{
			Window: &window.Window{
				Name:   "terminal",
				Title:  "Terminal",
				URL:    "/#/developer/terminal",
				Width:  800,
				Height: 500,
			},
		}},
	)))
}
func (s *Service) handleRun() {
	coreutil.DispatchAction(s.Core(), "display.handleRun", ActionIDECommand{Command: "run"})
}
func (s *Service) handleBuild() {
	coreutil.DispatchAction(s.Core(), "display.handleBuild", ActionIDECommand{Command: "build"})
}

// --- Tray (setup delegated via IPC) ---

func (s *Service) setupTray() {
	coreutil.ObserveResult(s.Core(), "display.setupTray", "tray setup failed", s.Core().Action("systray.set_menu").Run(context.Background(), core.NewOptions(
		core.Option{Key: "task", Value: systray.TaskSetTrayMenu{Items: []systray.TrayMenuItem{
			{Label: "Open Desktop", ActionID: "open-desktop"},
			{Label: "Close Desktop", ActionID: "close-desktop"},
			{Type: "separator"},
			{Label: "Environment Info", ActionID: "env-info"},
			{Type: "separator"},
			{Label: "Quit", ActionID: "quit"},
		}}},
	)))
}
