// pkg/mcp/subsystem.go
package mcp

import (
	"context"
	"reflect"
	"sort"
	"sync"
	"time"

	core "dappco.re/go"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Subsystem translates MCP tool calls to Core IPC messages for GUI operations.
type Subsystem struct {
	core  *core.Core
	mu    sync.RWMutex
	tools map[string]toolRecord
}

// ToolDescriptor is the chat-facing manifest entry for a GUI MCP tool.
type ToolDescriptor struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

type toolRecord struct {
	descriptor ToolDescriptor
	call       func(context.Context, map[string]any) (string, resultFailure)
}

// New(c) creates a display MCP subsystem backed by a Core instance.
// sub := mcp.New(c); sub.RegisterTools(server)
func New(c *core.Core) *Subsystem {
	return &Subsystem{
		core:  c,
		tools: make(map[string]toolRecord),
	}
}

func (s *Subsystem) Name() string { return "display" }

func (s *Subsystem) RegisterTools(server *mcp.Server) {
	s.registerDisplayTools(server)
	s.registerChatTools(server)
	s.registerWebviewTools(server)
	s.registerWindowTools(server)
	s.registerLayoutTools(server)
	s.registerScreenTools(server)
	s.registerClipboardTools(server)
	s.registerDialogTools(server)
	s.registerNotificationTools(server)
	s.registerTrayTools(server)
	s.registerEnvironmentTools(server)
	s.registerBrowserTools(server)
	s.registerContextMenuTools(server)
	s.registerKeybindingTools(server)
	s.registerDockTools(server)
	s.registerLifecycleTools(server)
	s.registerMarketplaceTools(server)
	s.registerEventsTools(server)
	s.registerMenuTools(server)
	s.registerP2PTools(server)
	s.registerDenoTools(server)
	s.registerContainerTools(server)
}

// Manifest returns the recorded MCP tool metadata in stable name order.
func (s *Subsystem) Manifest() []ToolDescriptor {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]ToolDescriptor, 0, len(s.tools))
	for _, record := range s.tools {
		result = append(result, record.descriptor)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// ManifestText renders the recorded MCP tool metadata as a system-prompt block.
func (s *Subsystem) ManifestText() string {
	manifest := s.Manifest()
	if len(manifest) == 0 {
		return ""
	}

	builder := core.NewBuilder()
	builder.WriteString("Available MCP tools:\n")
	for _, tool := range manifest {
		builder.WriteString("- ")
		builder.WriteString(tool.Name)
		if tool.Description != "" {
			builder.WriteString(": ")
			builder.WriteString(tool.Description)
		}
		schema := tool.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		builder.WriteString("\n  input_schema: ")
		builder.WriteString(core.JSONMarshalString(schema))
		builder.WriteString("\n")
	}
	return core.Trim(builder.String())
}

// CallTool executes a recorded GUI MCP tool directly by name.
func (s *Subsystem) CallTool(ctx context.Context, name string, arguments map[string]any) (string, resultFailure) {
	s.mu.RLock()
	record, ok := s.tools[name]
	s.mu.RUnlock()
	if !ok {
		return "", core.E("mcp.CallTool", "tool not found: "+name, nil)
	}
	if arguments == nil {
		arguments = map[string]any{}
	}
	return record.call(ctx, arguments)
}

func addTool[In, Out any](
	s *Subsystem,
	server *mcp.Server,
	tool *mcp.Tool,
	handler func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, resultFailure),
) {
	if tool.InputSchema == nil {
		tool.InputSchema = schemaForValue(new(In))
		if tool.InputSchema == nil {
			tool.InputSchema = map[string]any{"type": "object"}
		}
	}

	mcp.AddTool(server, tool, func(ctx context.Context, req *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Out, error) {
		result, output, err := handler(ctx, req, input)
		if err != nil {
			return nil, output, err
		}
		return result, output, nil
	})
	s.recordTool(tool, func(ctx context.Context, arguments map[string]any) (string, resultFailure) {
		var input In
		if len(arguments) > 0 {
			result := core.JSONUnmarshalString(core.JSONMarshalString(arguments), &input)
			if !result.OK {
				if err, ok := result.Value.(resultFailure); ok {
					return "", err
				}
				return "", core.E("mcp.addTool", "failed to decode tool input", nil)
			}
		}

		callResult, output, err := handler(ctx, nil, input)
		if err != nil {
			return "", err
		}
		if callResult != nil {
			return renderCallToolResult(callResult), nil
		}
		return core.JSONMarshalString(output), nil
	})
}

func (s *Subsystem) recordTool(tool *mcp.Tool, call func(context.Context, map[string]any) (string, resultFailure)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[tool.Name] = toolRecord{
		descriptor: ToolDescriptor{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: normalizeSchema(tool.InputSchema),
		},
		call: call,
	}
}

func renderCallToolResult(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}

	parts := make([]string, 0, len(result.Content))
	for _, content := range result.Content {
		switch value := content.(type) {
		case *mcp.TextContent:
			parts = append(parts, value.Text)
		default:
			parts = append(parts, core.JSONMarshalString(value))
		}
	}
	if len(parts) == 0 {
		return core.JSONMarshalString(result)
	}
	return core.Join("\n", parts...)
}

func normalizeSchema(schema any) map[string]any {
	switch value := schema.(type) {
	case map[string]any:
		return value
	case nil:
		return nil
	default:
		var result map[string]any
		unmarshal := core.JSONUnmarshalString(core.JSONMarshalString(value), &result)
		if !unmarshal.OK {
			return nil
		}
		return result
	}
}

func schemaForValue(value any) map[string]any {
	return schemaForType(reflect.TypeOf(value))
}

func schemaForType(t reflect.Type) map[string]any {
	if t == nil {
		return nil
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return nil
	}

	if t == reflect.TypeFor[time.Time]() {
		return map[string]any{
			"type":   "string",
			"format": "date-time",
		}
	}

	switch t.Kind() {
	case reflect.Struct:
		properties := make(map[string]any)
		required := make([]string, 0, t.NumField())
		for field := range t.Fields() {
			field := field
			if !field.IsExported() {
				continue
			}
			tag := field.Tag.Get("json")
			if tag == "-" {
				continue
			}
			name, optional := schemaFieldName(field.Name, tag)
			properties[name] = schemaForType(field.Type)
			if !optional {
				required = append(required, name)
			}
		}
		schema := map[string]any{
			"type":       "object",
			"properties": properties,
		}
		if len(required) > 0 {
			schema["required"] = required
		}
		return schema
	case reflect.Slice, reflect.Array:
		return map[string]any{
			"type":  "array",
			"items": schemaForType(t.Elem()),
		}
	case reflect.Map:
		return map[string]any{
			"type": "object",
		}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Interface:
		return map[string]any{}
	default:
		return map[string]any{"type": "string"}
	}
}

func schemaFieldName(fallback, tag string) (string, bool) {
	if tag == "" {
		return fallback, false
	}
	parts := core.Split(tag, ",")
	name := parts[0]
	if name == "" {
		name = fallback
	}
	optional := false
	for _, part := range parts[1:] {
		if part == "omitempty" {
			optional = true
		}
	}
	return name, optional
}
