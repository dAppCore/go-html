// SPDX-Licence-Identifier: EUPL-1.2

package ctml

import (
	"reflect"
	"strconv"
	"strings"

	html "dappco.re/go/render/engine/html"
)

// astNode is the parsed, pre-materialisation representation of one .ctml
// construct. It carries enough information for materialise to build the
// real html.Node tree; it never becomes a Node itself, so the parser never
// needs a custom Node implementation of its own -- see docs/ctml.md S:S1.2
// for why a custom Node type would be unsafe under the terminal renderer.
type astNode interface{ isAstNode() }

type astEl struct {
	Tag      string
	Attrs    []astAttr
	Children []astNode
}

// astAttr is one element attribute (S:S5). Value is the literal attribute
// string for the common static case; when the source value carries {{path}}
// tokens (S:S8.3) Parts holds the interleaved literal/bind segments, resolved
// per row at materialise time so an <each> row can carry a row-scoped class or
// id without any expression language -- every token is a named lookup, exactly
// like a text-run bind. The attribute stays static per constructed node: Each
// builds a fresh node per row, so each row's node has one fixed attribute
// value, and nothing re-evaluates an attribute per Render call (S:S1.3).
type astAttr struct {
	Key   string
	Value string    // literal fast path -- used when Parts == nil
	Parts []attrSeg // non-nil only when Value carried {{path}} tokens
}

// attrSeg is one segment of an interpolated attribute value: literal text
// (Lit), or a {{path}} reference (Path) resolved against the active each row /
// Bindings.Values scope at materialise time, a miss rendering empty (S:S8.3).
type attrSeg struct {
	Lit    string
	Path   string
	IsPath bool
}

// astText is a literal i18n key (S:S6.1); astBind is a {{path}} value
// reference (S:S8.3). Both may carry args (S:S6.4).
type astText struct {
	Key  string
	Args []argToken
}

// astBind is a {{path}} value reference, optionally extended with a single
// formatting pipe (S:S8.7): {{path}}, {{path|pipe}}, or {{path|pipe:arg}}.
// Pipe is "" for a plain, unpiped bind -- the pre-existing behaviour is
// unchanged. PipeArg is "" when the pipe carries no colon-argument.
type astBind struct {
	Path    string
	Args    []argToken
	Pipe    string
	PipeArg string
}

type astRaw struct{ Content string }

// astVerbatim carries pre-styled terminal bytes. A document-level
// <verbatim value="key"/> resolves Content from Bindings.Values at parse time
// (S:S6.5); a <verbatim value="{{path}}"/> defers to materialise time, binding
// Path against the enclosing <each> row (or Bindings.Values at document scope),
// a miss rendering empty -- exactly like a row-scoped {{path}} bind (S:S8.3), so
// an <each> can carry per-row pre-styled content. It materialises to
// html.Verbatim either way; the terminal renderer emits the bytes unchanged.
// The {{path}} form may also carry a single formatting pipe (S:S8.7), exactly
// like a text-run bind -- Pipe/PipeArg are always "" for the plain value="key"
// form, which has no {{path}} token to carry one.
type astVerbatim struct {
	Content string // parse-time-resolved literal content (plain value="key")
	Path    string // non-empty: a {{path}} bind resolved at materialise time
	Pipe    string
	PipeArg string
}

type astIf struct {
	CondKey string
	Child   astNode
}

type astUnless struct {
	CondKey string
	Child   astNode
}

type astSwitch struct {
	OnKey string
	Cases map[string]astNode
}

type astEntitled struct {
	Feature string
	Child   astNode
}

type astEach struct {
	ItemsName string
	AsName    string
	Body      astNode
}

// astFragment holds multiple siblings under a wrapper (If/Unless/Entitled/
// case) that only accepts one Node -- see fragment().
type astFragment struct{ Children []astNode }

type astLayout struct {
	Variant string
	Slots   map[byte][]astNode // keys are always 'H','L','C','R','F'
}

type astResponsive struct{ Variants []astVariant }

type astVariant struct {
	Name   string
	Media  string
	Layout *astLayout
}

func (*astEl) isAstNode()         {}
func (*astText) isAstNode()       {}
func (*astBind) isAstNode()       {}
func (*astRaw) isAstNode()        {}
func (*astVerbatim) isAstNode()   {}
func (*astIf) isAstNode()         {}
func (*astUnless) isAstNode()     {}
func (*astSwitch) isAstNode()     {}
func (*astEntitled) isAstNode()   {}
func (*astEach) isAstNode()       {}
func (*astFragment) isAstNode()   {}
func (*astLayout) isAstNode()     {}
func (*astResponsive) isAstNode() {}

// argToken is one comma-separated entry in an args="..." attribute: either
// a literal string or a {{path}} reference resolved against the active
// each scope at materialisation time (S:S6.4).
type argToken struct {
	Lit    string
	Path   string
	IsPath bool
}

// resolver looks a dotted {{path}} reference up in the active scope chain:
// the nearest enclosing <each> row whose as-name the path names, falling
// through to Bindings.Values at document scope (valuesResolver). ok is false
// for an unresolvable path -- an absent field, or an absent Values key --
// which materialise renders as empty text: data absence, not a document
// defect (docs/ctml.md S:S8.3).
type resolver func(path string) (any, bool)

// fragment collapses multiple sibling nodes into the single Node that If,
// Unless, Entitled, and each Switch case require. It reuses the real,
// exported Each constructor purely for its no-wrapper concatenation
// behaviour: Each(nodes, identity) renders each item's own output back to
// back with no extra markup and no new node type, so (being the genuine
// Each) it is already safe under RenderTerm for any item type -- see
// docs/ctml.md S:S6.3. A single child is returned unwrapped.
func fragment(nodes []html.Node) html.Node {
	switch len(nodes) {
	case 0:
		return html.Raw("")
	case 1:
		return nodes[0]
	default:
		return html.Each(nodes, func(n html.Node) html.Node { return n })
	}
}

func materialiseAll(nodes []astNode, resolve resolver, bnd Bindings) []html.Node {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]html.Node, len(nodes))
	for i, n := range nodes {
		out[i] = materialise(n, resolve, bnd)
	}
	return out
}

// materialise builds the real html.Node tree for one astNode, calling only
// exported dappco.re/go/render constructors -- see docs/ctml.md S:S1.2.
func materialise(n astNode, resolve resolver, bnd Bindings) html.Node {
	switch t := n.(type) {
	case *astEl:
		node := html.El(t.Tag, materialiseAll(t.Children, resolve, bnd)...)
		for _, a := range t.Attrs {
			node = html.Attr(node, a.Key, resolveAttr(a, resolve))
		}
		return node
	case *astText:
		return html.Text(t.Key, resolveArgs(t.Args, resolve)...)
	case *astBind:
		v, ok := resolve(t.Path)
		if t.Pipe != "" {
			return html.Text(pipedStringOf(t.Pipe, v, t.PipeArg, ok), resolveArgs(t.Args, resolve)...)
		}
		return html.Text(stringOf(v), resolveArgs(t.Args, resolve)...)
	case *astRaw:
		return html.Raw(t.Content)
	case *astVerbatim:
		// A row-scoped (or document-scoped) {{path}} binds at materialise time
		// via the active resolver -- the enclosing <each> row, else Values -- a
		// miss stringifying to "" exactly like an astBind (S:S8.3). A plain
		// value="key" carries its parse-time-resolved Content straight through.
		if t.Path != "" {
			v, ok := resolve(t.Path)
			if t.Pipe != "" {
				return html.Verbatim(pipedStringOf(t.Pipe, v, t.PipeArg, ok))
			}
			return html.Verbatim(stringOf(v))
		}
		return html.Verbatim(t.Content)
	case *astIf:
		return html.If(dataTruthyFunc(t.CondKey), materialise(t.Child, resolve, bnd))
	case *astUnless:
		return html.Unless(dataTruthyFunc(t.CondKey), materialise(t.Child, resolve, bnd))
	case *astSwitch:
		cases := make(map[string]html.Node, len(t.Cases))
		for value, child := range t.Cases {
			cases[value] = materialise(child, resolve, bnd)
		}
		return html.Switch(dataStringFunc(t.OnKey), cases)
	case *astEntitled:
		return html.Entitled(t.Feature, materialise(t.Child, resolve, bnd))
	case *astEach:
		return materialiseEach(t, resolve, bnd)
	case *astFragment:
		return fragment(materialiseAll(t.Children, resolve, bnd))
	case *astLayout:
		return materialiseLayout(t, resolve, bnd)
	case *astResponsive:
		return materialiseResponsive(t, resolve, bnd)
	default:
		return html.Raw("")
	}
}

func materialiseEach(t *astEach, resolve resolver, bnd Bindings) html.Node {
	items := bnd.Sequences[t.ItemsName]
	asName := t.AsName
	body := t.Body
	return html.Each(items, func(row map[string]any) html.Node {
		child := func(path string) (any, bool) {
			if rest, ok := stripEachPrefix(path, asName); ok {
				return lookupPath(row, rest)
			}
			return resolve(path)
		}
		return materialise(body, child, bnd)
	})
}

func materialiseLayout(t *astLayout, resolve resolver, bnd Bindings) *html.Layout {
	return html.NewLayout(t.Variant).
		H(materialiseAll(t.Slots['H'], resolve, bnd)...).
		L(materialiseAll(t.Slots['L'], resolve, bnd)...).
		C(materialiseAll(t.Slots['C'], resolve, bnd)...).
		R(materialiseAll(t.Slots['R'], resolve, bnd)...).
		F(materialiseAll(t.Slots['F'], resolve, bnd)...)
}

func materialiseResponsive(t *astResponsive, resolve resolver, bnd Bindings) *html.Responsive {
	r := html.NewResponsive()
	for _, v := range t.Variants {
		layout := materialiseLayout(v.Layout, resolve, bnd)
		if v.Media != "" {
			r = r.Add(v.Name, layout, v.Media)
			continue
		}
		r = r.Add(v.Name, layout)
	}
	return r
}

// resolveAttr renders an attribute's value for the current scope: the literal
// string for a static attribute (Parts == nil), or the {{path}} template
// resolved segment by segment -- each bind via the active resolver (the
// enclosing <each> row, else Bindings.Values), a miss rendering empty, exactly
// like an astBind (S:S8.3). The result is a plain static string handed to
// html.Attr, so the constructed node's attribute is fixed; an <each> gains
// per-row attribute values only because Each constructs a fresh node per row
// (S:S5). Values are stringified with stringOf, not translated -- an attribute
// is a class/id/href, never an i18n key.
func resolveAttr(a astAttr, resolve resolver) string {
	if a.Parts == nil {
		return a.Value
	}
	var b strings.Builder
	for _, seg := range a.Parts {
		if !seg.IsPath {
			b.WriteString(seg.Lit)
			continue
		}
		v, _ := resolve(seg.Path)
		b.WriteString(stringOf(v))
	}
	return b.String()
}

func resolveArgs(args []argToken, resolve resolver) []any {
	if len(args) == 0 {
		return nil
	}
	out := make([]any, len(args))
	for i, a := range args {
		if a.IsPath {
			v, _ := resolve(a.Path)
			out[i] = v
			continue
		}
		out[i] = a.Lit
	}
	return out
}

// valuesResolver backs every {{path}} lookup that reaches document scope --
// a token outside all <each> bodies, or one inside a body whose root name
// matches no enclosing <each as="...">. It walks the dotted path into
// Bindings.Values with the same lookupPath semantics an <each> row uses, so
// {{user}} is Values["user"] and {{user.name}} indexes one level in. A miss
// (absent key, or a nil Values map) returns ok=false, which materialise
// renders as empty text -- data absence, not a document defect (S:S8.3).
func valuesResolver(values map[string]any) resolver {
	return func(path string) (any, bool) {
		return lookupPath(values, path)
	}
}

func stripEachPrefix(path, asName string) (string, bool) {
	if path == asName {
		return "", true
	}
	prefix := asName + "."
	if strings.HasPrefix(path, prefix) {
		return path[len(prefix):], true
	}
	return "", false
}

func lookupPath(item map[string]any, path string) (any, bool) {
	if item == nil || path == "" {
		return nil, false
	}
	var current any = item
	for step := range strings.SplitSeq(path, ".") {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok := m[step]
		if !ok {
			return nil, false
		}
		current = v
	}
	return current, true
}

func dataTruthyFunc(key string) func(*html.Context) bool {
	return func(ctx *html.Context) bool {
		if ctx == nil || ctx.Data == nil {
			return false
		}
		v, ok := ctx.Data[key]
		if !ok {
			return false
		}
		return truthy(v)
	}
}

func dataStringFunc(key string) func(*html.Context) string {
	return func(ctx *html.Context) string {
		if ctx == nil || ctx.Data == nil {
			return ""
		}
		v, ok := ctx.Data[key]
		if !ok {
			return ""
		}
		s, ok := v.(string)
		if !ok {
			return ""
		}
		return s
	}
}

// truthy applies the coercion documented in docs/ctml.md S:S7.1: no
// operators, just a fixed per-kind presence/zero rule. The explicit cases
// cover the common path without reflection; reflect.Value handles every
// other numeric/container kind so a caller's own named types still work.
func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case int:
		return t != 0
	case int64:
		return t != 0
	case float64:
		return t != 0
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Map, reflect.Array, reflect.String:
		return rv.Len() > 0
	case reflect.Bool:
		return rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() != 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint() != 0
	case reflect.Float32, reflect.Float64:
		return rv.Float() != 0
	case reflect.Pointer, reflect.Interface:
		return !rv.IsNil()
	default:
		return true
	}
}

// stringOf renders a resolved data value as display text for astBind. An
// unsupported type (a nested map or slice) renders as empty rather than
// guessing a representation -- see docs/ctml.md S:S8.3.
func stringOf(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'g', -1, 32)
	default:
		return ""
	}
}

// pipedStringOf formats a resolved {{path|pipe}} value through go-html's
// Formatter default (S:S8.7). materialise resolves every bind at
// construction time (S:S1.3, S:S8.1), so there is no *html.Context here to
// route through the Formatter seam -- html.FormatValue is the exported,
// Context-free entry point that seam falls back to as well, so a ctml pipe
// and a hand-built tree with no Formatter installed format identically. A
// resolver miss (ok false) renders empty, exactly like an unpiped bind's
// stringOf(nil) -- a missing field must stay data absence (S:S8.3), not a
// formatted zero.
func pipedStringOf(pipe string, v any, arg string, ok bool) string {
	if !ok {
		return ""
	}
	if arg == "" {
		return html.FormatValue(pipe, v)
	}
	return html.FormatValue(pipe, v, arg)
}
