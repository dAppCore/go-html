// SPDX-Licence-Identifier: EUPL-1.2

package ctml

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"

	core "dappco.re/go"
	html "dappco.re/go/render/engine/html"
)

// parser walks one document with encoding/xml.Decoder. A {{path}} token is
// always syntactically valid: it resolves at materialise time against the
// nearest enclosing <each> row whose as-name it names, or against
// Bindings.Values at document scope, with a miss rendering as empty text
// (docs/ctml.md S:S8). There is therefore no parse-time scope stack to keep.
// bnd is held so <verbatim value="key"/> can resolve its pre-styled content
// from Bindings.Values at parse time (S:S6.5).
type parser struct {
	dec *xml.Decoder
	bnd Bindings
}

// Parse parses src into the node tree the Go builder API would produce for
// the same page -- see docs/ctml.md for the grammar. bindings is optional;
// an <each> whose items name is absent from Sequences renders as an empty
// list rather than failing to parse.
//
// Usage example: tree, err := ctml.Parse(src)
func Parse(src []byte, bindings ...Bindings) (html.Node, error) {
	root, bnd, err := parseRoot(src, bindings)
	if err != nil {
		return nil, err
	}
	return materialise(root, valuesResolver(bnd.Values), bnd), nil
}

// ParseLayout is Parse specialised to documents whose root is <layout>,
// returning the concrete *html.Layout so callers can chain further
// builder calls (Responsive.Add, further slot appends) or call
// Layout-specific methods (RenderTerm) directly.
//
// Usage example: layout, err := ctml.ParseLayout(src)
func ParseLayout(src []byte, bindings ...Bindings) (*html.Layout, error) {
	root, bnd, err := parseRoot(src, bindings)
	if err != nil {
		return nil, err
	}
	l, ok := root.(*astLayout)
	if !ok {
		msg := "root element must be <layout>"
		return nil, &ParseError{Line: 1, Col: 1, Msg: msg, Cause: core.E("ctml.ParseLayout", msg, nil)}
	}
	return materialiseLayout(l, valuesResolver(bnd.Values), bnd), nil
}

func parseRoot(src []byte, bindings []Bindings) (astNode, Bindings, error) {
	var bnd Bindings
	if len(bindings) > 0 {
		bnd = bindings[0]
	}

	p := &parser{dec: xml.NewDecoder(bytes.NewReader(src)), bnd: bnd}

	for {
		tok, err := p.dec.Token()
		if err != nil {
			return nil, bnd, p.wrapXMLErr(err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			root, err := p.parseElement(t)
			if err != nil {
				return nil, bnd, err
			}
			if err := p.expectTrailingEOF(); err != nil {
				return nil, bnd, err
			}
			return root, bnd, nil
		case xml.CharData:
			if strings.TrimSpace(string(t)) != "" {
				return nil, bnd, p.errAt("unexpected text before root element")
			}
		case xml.Comment, xml.ProcInst, xml.Directive:
			continue
		}
	}
}

// expectTrailingEOF confirms a document has exactly one root element --
// anything but insignificant whitespace/comments after it closes is a
// document defect, not silently-ignored trailing content.
func (p *parser) expectTrailingEOF() error {
	for {
		tok, err := p.dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return p.wrapXMLErr(err)
		}
		switch t := tok.(type) {
		case xml.CharData:
			if strings.TrimSpace(string(t)) != "" {
				return p.errAt("unexpected content after root element")
			}
		case xml.Comment, xml.ProcInst, xml.Directive:
			continue
		default:
			return p.errAt("unexpected content after root element")
		}
	}
}

// parseElement dispatches on the sixteen reserved tag names (S:S3); every
// other tag is a literal element (parseEl).
func (p *parser) parseElement(start xml.StartElement) (astNode, error) {
	switch start.Name.Local {
	case "if":
		return p.parseIf(start)
	case "unless":
		return p.parseUnless(start)
	case "switch":
		return p.parseSwitch(start)
	case "case":
		return nil, p.errAt("<case> is only valid as a direct child of <switch>")
	case "entitled":
		return p.parseEntitled(start)
	case "each":
		return p.parseEach(start)
	case "raw":
		return p.parseRaw(start)
	case "verbatim":
		return p.parseVerbatim(start)
	case "layout":
		return p.parseLayout(start)
	case "h", "l", "c", "r", "f":
		return nil, p.errAt("<" + start.Name.Local + "> is only valid as a direct child of <layout>")
	case "responsive":
		return p.parseResponsive(start)
	case "variant":
		return nil, p.errAt("<variant> is only valid as a direct child of <responsive>")
	default:
		return p.parseEl(start)
	}
}

func (p *parser) parseEl(start xml.StartElement) (astNode, error) {
	var attrs []astAttr
	argsAttr, hasArgs := "", false
	for _, a := range start.Attr {
		if a.Name.Local == "args" {
			argsAttr, hasArgs = a.Value, true
			continue
		}
		parts, err := p.splitAttrValue(a.Value)
		if err != nil {
			return nil, err
		}
		attrs = append(attrs, astAttr{Key: a.Name.Local, Value: a.Value, Parts: parts})
	}

	children, err := p.parseContent(start.Name)
	if err != nil {
		return nil, err
	}
	if hasArgs {
		if err := p.applyArgs(children, argsAttr); err != nil {
			return nil, err
		}
	}
	return &astEl{Tag: start.Name.Local, Attrs: attrs, Children: children}, nil
}

// applyArgs attaches args="..." to the element's sole text/bind child --
// S:S6.4 restricts args to single-run content, so mixed or element content
// is a parse error rather than a silently-dropped attribute.
func (p *parser) applyArgs(children []astNode, raw string) error {
	if len(children) != 1 {
		return p.errAt("args attribute requires exactly one text child")
	}
	tokens := p.parseArgTokens(raw)
	switch t := children[0].(type) {
	case *astText:
		t.Args = tokens
	case *astBind:
		t.Args = tokens
	default:
		return p.errAt("args attribute requires text content, not an element child")
	}
	return nil
}

// parseArgTokens splits an args="..." value into its comma-separated tokens
// (S:S6.4). Each token is either a literal string or a whole {{path}}
// reference resolved at materialise time against the active scope chain
// (the enclosing <each> row first, then Bindings.Values). A near-miss like
// {{oops!}} is not a valid path token, so it stays a literal argument.
func (p *parser) parseArgTokens(raw string) []argToken {
	parts := strings.Split(raw, ",")
	tokens := make([]argToken, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if path, ok := matchBindToken(trimmed); ok {
			tokens = append(tokens, argToken{Path: path, IsPath: true})
			continue
		}
		tokens = append(tokens, argToken{Lit: trimmed})
	}
	return tokens
}

// parseContent reads children until the EndElement matching startName,
// dispatching StartElements recursively and turning each CharData run
// into a text or bind node (S:S2). It is shared by every construct whose
// children are ordinary mixed content; <switch>, <raw>, <layout>, and
// <responsive> have their own loops because each restricts its children
// to one specific reserved element.
func (p *parser) parseContent(startName xml.Name) ([]astNode, error) {
	var nodes []astNode
	for {
		tok, err := p.dec.Token()
		if err != nil {
			return nil, p.wrapXMLErr(err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			child, err := p.parseElement(t)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, child)
		case xml.EndElement:
			if t.Name == startName {
				return nodes, nil
			}
			return nil, p.errAt("mismatched closing tag")
		case xml.CharData:
			raw := string(t)
			if strings.TrimSpace(raw) == "" {
				continue // pure structural whitespace between siblings
			}
			segs, err := p.splitRun(normaliseRun(raw))
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, segs...)
		case xml.Comment, xml.ProcInst, xml.Directive:
			continue
		}
	}
}

// splitRun turns one CharData run (already edge-normalised, S:S2) into the
// interleaved Text and bind nodes it denotes. Each {{path}} token within the
// run becomes an astBind resolved at materialise time (against the enclosing
// <each> row or Bindings.Values, S:S8.3); the literal text between tokens
// becomes astText. The whitespace edge rule is applied to the run as a whole
// before this split (the caller passes normaliseRun's output), not per
// segment, and empty text segments are dropped -- so "○ {{tab.label}}"
// becomes Text("○ ") + bind, and a whole-run "{{x}}" becomes a lone bind.
// A "{{" that opens no valid {{path}} token stays literal text (S:S8.4): the
// closed vocabulary makes a well-formed {{path}} always a lookup, so there is
// no escape syntax to invent. A token may also carry a single formatting
// pipe (S:S8.7, {{path|pipe}} / {{path|pipe:arg}}); an unrecognised pipe
// name is a loud parse error, not a literal fallback.
func (p *parser) splitRun(run string) ([]astNode, error) {
	var nodes []astNode
	segStart, i := 0, 0
	for i < len(run) {
		if run[i] == '{' && i+1 < len(run) && run[i+1] == '{' {
			tok, end, ok, pipeErr := scanBindToken(run, i)
			if pipeErr != "" {
				return nil, p.errAt(pipeErr)
			}
			if ok {
				if seg := run[segStart:i]; seg != "" {
					nodes = append(nodes, &astText{Key: seg})
				}
				nodes = append(nodes, &astBind{Path: tok.Path, Pipe: tok.Pipe, PipeArg: tok.Arg})
				i, segStart = end, end
				continue
			}
		}
		i++
	}
	if seg := run[segStart:]; seg != "" {
		nodes = append(nodes, &astText{Key: seg})
	}
	return nodes, nil
}

// splitAttrValue splits an attribute value into the literal/bind segments a
// {{path}} interpolation denotes, using the same token recognition splitRun
// applies to text runs (S:S8.3) -- but producing raw string segments, because
// an attribute value is a class/id/href and never an i18n key, so a bind
// resolves straight to its string form at materialise time. It returns nil
// when the value carries no valid {{path}} token, so a static attribute keeps
// its literal fast path with no per-render work -- the overwhelmingly common
// case. The row scope this resolves against is what lets an <each> row carry a
// row-scoped class or id (S:S5); a "{{" that opens no valid path token stays
// literal, exactly as in a text run (S:S8.4). A formatting pipe (S:S8.7) is
// NOT a pipe-legal context here -- an attribute is code, not copy -- so any
// {{path|pipe}} token, well-formed or not, is a loud parse error rather than
// either interpolating or falling back to literal text.
func (p *parser) splitAttrValue(value string) ([]attrSeg, error) {
	if !strings.Contains(value, "{{") {
		return nil, nil
	}
	var segs []attrSeg
	segStart, i := 0, 0
	for i < len(value) {
		if value[i] == '{' && i+1 < len(value) && value[i+1] == '{' {
			tok, end, ok, pipeErr := scanBindToken(value, i)
			if pipeErr != "" {
				return nil, p.errAt("an attribute bind must not use a pipe (S:S8.7): " + pipeErr)
			}
			if ok {
				if tok.Pipe != "" {
					return nil, p.errAt("an attribute bind must not use a pipe (S:S8.7): {{" + value[i+2:end-2] + "}}")
				}
				if seg := value[segStart:i]; seg != "" {
					segs = append(segs, attrSeg{Lit: seg})
				}
				segs = append(segs, attrSeg{Path: tok.Path, IsPath: true})
				i, segStart = end, end
				continue
			}
		}
		i++
	}
	if len(segs) == 0 {
		return nil, nil // "{{" present but no valid token -- keep the literal fast path
	}
	if seg := value[segStart:]; seg != "" {
		segs = append(segs, attrSeg{Lit: seg})
	}
	return segs, nil
}

// normaliseRun strips a CharData run's leading/trailing whitespace only
// when that edge whitespace contains a newline -- i.e. only when it is
// source-formatting indentation. A plain inline space at the edge (as in
// "Hello " before a following <strong>) is significant text-flow spacing
// and is left untouched, so mixed content round-trips through .ctml with
// the same spacing an author typed. A run that is whitespace-only never
// reaches this function -- parseContent drops it entirely first.
func normaliseRun(s string) string {
	start := 0
	for start < len(s) && isRunWS(s[start]) {
		start++
	}
	end := len(s)
	for end > start && isRunWS(s[end-1]) {
		end--
	}
	if !strings.Contains(s[:start], "\n") {
		start = 0
	}
	if !strings.Contains(s[end:], "\n") {
		end = len(s)
	}
	return s[start:end]
}

func isRunWS(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

// singleChild collapses parsed children to the one Node that If, Unless,
// Entitled, and <case> require, wrapping multiples in astFragment (S:S6.3).
func (p *parser) singleChild(nodes []astNode) (astNode, error) {
	switch len(nodes) {
	case 0:
		return nil, p.errAt("expected at least one child")
	case 1:
		return nodes[0], nil
	default:
		return &astFragment{Children: nodes}, nil
	}
}

func (p *parser) parseIf(start xml.StartElement) (astNode, error) {
	cond, ok := attrValue(start, "cond")
	if !ok {
		return nil, p.errAt("<if> requires a cond attribute")
	}
	children, err := p.parseContent(start.Name)
	if err != nil {
		return nil, err
	}
	child, err := p.singleChild(children)
	if err != nil {
		return nil, err
	}
	return &astIf{CondKey: cond, Child: child}, nil
}

func (p *parser) parseUnless(start xml.StartElement) (astNode, error) {
	cond, ok := attrValue(start, "cond")
	if !ok {
		return nil, p.errAt("<unless> requires a cond attribute")
	}
	children, err := p.parseContent(start.Name)
	if err != nil {
		return nil, err
	}
	child, err := p.singleChild(children)
	if err != nil {
		return nil, err
	}
	return &astUnless{CondKey: cond, Child: child}, nil
}

func (p *parser) parseEntitled(start xml.StartElement) (astNode, error) {
	feature, ok := attrValue(start, "feature")
	if !ok {
		return nil, p.errAt("<entitled> requires a feature attribute")
	}
	children, err := p.parseContent(start.Name)
	if err != nil {
		return nil, err
	}
	child, err := p.singleChild(children)
	if err != nil {
		return nil, err
	}
	return &astEntitled{Feature: feature, Child: child}, nil
}

func (p *parser) parseEach(start xml.StartElement) (astNode, error) {
	items, ok := attrValue(start, "items")
	if !ok {
		return nil, p.errAt("<each> requires an items attribute")
	}
	as, ok := attrValue(start, "as")
	if !ok {
		return nil, p.errAt("<each> requires an as attribute")
	}

	children, err := p.parseContent(start.Name)
	if err != nil {
		return nil, err
	}
	body, err := p.singleChild(children)
	if err != nil {
		return nil, err
	}
	return &astEach{ItemsName: items, AsName: as, Body: body}, nil
}

func (p *parser) parseSwitch(start xml.StartElement) (astNode, error) {
	on, ok := attrValue(start, "on")
	if !ok {
		return nil, p.errAt("<switch> requires an on attribute")
	}

	cases := make(map[string]astNode)
	for {
		tok, err := p.dec.Token()
		if err != nil {
			return nil, p.wrapXMLErr(err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "case" {
				return nil, p.errAt("<switch> only accepts <case> children")
			}
			value, ok := attrValue(t, "value")
			if !ok {
				return nil, p.errAt("<case> requires a value attribute")
			}
			if _, dup := cases[value]; dup {
				return nil, p.errAt("duplicate <case value=\"" + value + "\">")
			}
			children, err := p.parseContent(t.Name)
			if err != nil {
				return nil, err
			}
			child, err := p.singleChild(children)
			if err != nil {
				return nil, err
			}
			cases[value] = child
		case xml.EndElement:
			if t.Name == start.Name {
				return &astSwitch{OnKey: on, Cases: cases}, nil
			}
			return nil, p.errAt("mismatched closing tag in <switch>")
		case xml.CharData:
			if strings.TrimSpace(string(t)) != "" {
				return nil, p.errAt("<switch> only accepts <case> children")
			}
		case xml.Comment, xml.ProcInst, xml.Directive:
			continue
		}
	}
}

func (p *parser) parseRaw(start xml.StartElement) (astNode, error) {
	var b strings.Builder
	for {
		tok, err := p.dec.Token()
		if err != nil {
			return nil, p.wrapXMLErr(err)
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.Write(t)
		case xml.EndElement:
			if t.Name == start.Name {
				return &astRaw{Content: b.String()}, nil
			}
			return nil, p.errAt("mismatched closing tag in <raw>")
		case xml.StartElement:
			return nil, p.errAt("<raw> cannot contain child elements")
		case xml.Comment, xml.ProcInst, xml.Directive:
			continue
		}
	}
}

// parseVerbatim reads <verbatim value="..."/>. A whole {{path}} token defers
// resolution to materialise time, binding against the enclosing <each> row (or
// Bindings.Values at document scope), a miss rendering empty -- exactly like a
// row-scoped {{path}} bind (S:S8.3), so an <each> can carry per-row pre-styled
// content. That {{path}} token may also carry a single formatting pipe
// (S:S8.7); an unrecognised pipe name is a loud parse error. Any other value
// is a plain Bindings.Values key resolved at parse time (S:S6.5): an absent
// key, or a non-string value, is a position-accurate parse error. Pre-styled
// ANSI/control bytes cannot live in XML markup, so the content arrives
// through the binding either way, and any child content is rejected.
func (p *parser) parseVerbatim(start xml.StartElement) (astNode, error) {
	key, ok := attrValue(start, "value")
	if !ok {
		return nil, p.errAt("<verbatim> requires a value attribute")
	}
	tok, isBind, pipeErr := matchPipedBindToken(key)
	if pipeErr != "" {
		return nil, p.errAt(pipeErr)
	}
	if isBind {
		if err := p.expectEmptyElement(start.Name); err != nil {
			return nil, err
		}
		return &astVerbatim{Path: tok.Path, Pipe: tok.Pipe, PipeArg: tok.Arg}, nil
	}
	raw, ok := p.bnd.Values[key]
	if !ok {
		return nil, p.errAt("<verbatim value=\"" + key + "\">: no such key in Bindings.Values")
	}
	content, ok := raw.(string)
	if !ok {
		return nil, p.errAt("<verbatim value=\"" + key + "\">: Bindings.Values[\"" + key + "\"] is not a string")
	}
	if err := p.expectEmptyElement(start.Name); err != nil {
		return nil, err
	}
	return &astVerbatim{Content: content}, nil
}

// expectEmptyElement consumes the body of a self-closing or empty element up
// to its matching end tag, rejecting any child element or non-whitespace
// text -- <verbatim>'s content comes from a binding, not from markup.
func (p *parser) expectEmptyElement(name xml.Name) error {
	for {
		tok, err := p.dec.Token()
		if err != nil {
			return p.wrapXMLErr(err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name == name {
				return nil
			}
			return p.errAt("mismatched closing tag")
		case xml.StartElement:
			return p.errAt("<verbatim> cannot contain child elements")
		case xml.CharData:
			if strings.TrimSpace(string(t)) != "" {
				return p.errAt("<verbatim> cannot contain text content")
			}
		case xml.Comment, xml.ProcInst, xml.Directive:
			continue
		}
	}
}

func (p *parser) parseLayout(start xml.StartElement) (astNode, error) {
	variant, ok := attrValue(start, "variant")
	if !ok {
		return nil, p.errAt("<layout> requires a variant attribute")
	}

	slots := make(map[byte][]astNode)
	for {
		tok, err := p.dec.Token()
		if err != nil {
			return nil, p.wrapXMLErr(err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			slot := slotLetter(t.Name.Local)
			if slot == 0 {
				return nil, p.errAt("<layout> only accepts <h> <l> <c> <r> <f> children")
			}
			children, err := p.parseContent(t.Name)
			if err != nil {
				return nil, err
			}
			slots[slot] = append(slots[slot], children...)
		case xml.EndElement:
			if t.Name == start.Name {
				return &astLayout{Variant: variant, Slots: slots}, nil
			}
			return nil, p.errAt("mismatched closing tag in <layout>")
		case xml.CharData:
			if strings.TrimSpace(string(t)) != "" {
				return nil, p.errAt("<layout> only accepts <h> <l> <c> <r> <f> children")
			}
		case xml.Comment, xml.ProcInst, xml.Directive:
			continue
		}
	}
}

func (p *parser) parseResponsive(start xml.StartElement) (astNode, error) {
	var variants []astVariant
	for {
		tok, err := p.dec.Token()
		if err != nil {
			return nil, p.wrapXMLErr(err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "variant" {
				return nil, p.errAt("<responsive> only accepts <variant> children")
			}
			name, ok := attrValue(t, "name")
			if !ok {
				return nil, p.errAt("<variant> requires a name attribute")
			}
			media, _ := attrValue(t, "media")
			children, err := p.parseContent(t.Name)
			if err != nil {
				return nil, err
			}
			layout, err := p.singleLayoutChild(children)
			if err != nil {
				return nil, err
			}
			variants = append(variants, astVariant{Name: name, Media: media, Layout: layout})
		case xml.EndElement:
			if t.Name == start.Name {
				return &astResponsive{Variants: variants}, nil
			}
			return nil, p.errAt("mismatched closing tag in <responsive>")
		case xml.CharData:
			if strings.TrimSpace(string(t)) != "" {
				return nil, p.errAt("<responsive> only accepts <variant> children")
			}
		case xml.Comment, xml.ProcInst, xml.Directive:
			continue
		}
	}
}

func (p *parser) singleLayoutChild(nodes []astNode) (*astLayout, error) {
	if len(nodes) != 1 {
		return nil, p.errAt("<variant> requires exactly one <layout> child")
	}
	l, ok := nodes[0].(*astLayout)
	if !ok {
		return nil, p.errAt("<variant> requires a <layout> child")
	}
	return l, nil
}

func (p *parser) errAt(msg string) error {
	line, col := p.dec.InputPos()
	return &ParseError{Line: line, Col: col, Msg: msg, Cause: core.E("ctml.Parse", msg, nil)}
}

func (p *parser) wrapXMLErr(err error) error {
	line, col := p.dec.InputPos()
	msg := "malformed XML"
	// A clean end of input surfaces as the io.EOF sentinel; a document that
	// runs off the end mid-tag surfaces as a *xml.SyntaxError wrapping the
	// text "unexpected EOF" instead -- both are the same defect from a
	// .ctml author's point of view (nothing to close the document with).
	if err == io.EOF || strings.Contains(err.Error(), "unexpected EOF") {
		msg = "unexpected end of document"
	}
	return &ParseError{Line: line, Col: col, Msg: msg, Cause: core.E("ctml.Parse", msg, err)}
}

func attrValue(start xml.StartElement, name string) (string, bool) {
	for _, a := range start.Attr {
		if a.Name.Local == name {
			return a.Value, true
		}
	}
	return "", false
}

func slotLetter(tag string) byte {
	switch tag {
	case "h":
		return 'H'
	case "l":
		return 'L'
	case "c":
		return 'C'
	case "r":
		return 'R'
	case "f":
		return 'F'
	default:
		return 0
	}
}

// matchBindToken recognises a whole trimmed run of the shape {{ident(.ident)*}}.
// Anything else -- including a near-miss like {{oops!}} -- is left as
// literal text (S:S8.4 is lenient by design: prose that merely contains
// double braces should not become a parse error). This is the pipe-blind
// matcher: it is used only by parseArgTokens, since args="..." is not a
// pipe-legal context (S:S8.7) -- a {{path|pipe}} token there fails
// isValidPath (the "|" is not a valid path character) and so falls through
// to a literal argument string exactly as before pipes existed. The
// pipe-aware matcher <verbatim> and text runs use is matchPipedBindToken.
func matchBindToken(s string) (string, bool) {
	if len(s) < 4 || !strings.HasPrefix(s, "{{") || !strings.HasSuffix(s, "}}") {
		return "", false
	}
	inner := strings.TrimSpace(s[2 : len(s)-2])
	if !isValidPath(inner) {
		return "", false
	}
	return inner, true
}

// bindToken is one recognised {{path}} token body: a dotted path, and
// (S:S8.7) an optional single formatting pipe with an optional single
// colon-argument -- {{path}}, {{path|pipe}}, or {{path|pipe:arg}}. Pipe is
// "" for a plain, unpiped token.
type bindToken struct {
	Path string
	Pipe string
	Arg  string
}

// pipeNames is the closed v1 formatting-pipe vocabulary (docs/ctml.md
// S:S8.7). A "|" inside a {{...}} token has no other meaning in CTML, so
// once one appears the token is unambiguously a pipe attempt: an
// unrecognised name is always a loud parse error (S:S9), never S:S8.4's
// near-miss-stays-literal leniency -- that leniency governs the path half
// only.
var pipeNames = map[string]bool{
	"number": true, "decimal": true, "percent": true,
	"ordinal": true, "ago": true, "size": true, "bytes": true,
}

// parseBindBody parses one {{...}} token's already-trimmed inner text into
// a path and an optional pipe (S:S8.7). ok is false when the path half is
// not a valid identifier path (S:S8.4 leniency, unaffected by pipes -- the
// caller treats the whole {{...}} as literal text). pipeErr is a non-empty
// reason when the path half checks out but the "|" pipe half does not (a
// pipe name that is not a plain identifier, or one outside pipeNames): the
// caller always surfaces this as a loud parse error via its own p.errAt,
// rather than falling through to literal text -- see scanBindToken.
func parseBindBody(inner string) (tok bindToken, ok bool, pipeErr string) {
	pathPart, pipePart, hasPipe := strings.Cut(inner, "|")
	path := strings.TrimSpace(pathPart)
	if !isValidPath(path) {
		return bindToken{}, false, ""
	}
	if !hasPipe {
		return bindToken{Path: path}, true, ""
	}
	namePart, argPart, hasArg := strings.Cut(pipePart, ":")
	name := strings.TrimSpace(namePart)
	if !isIdent(name) {
		return bindToken{}, false, "malformed pipe in {{" + inner + "}}"
	}
	if !pipeNames[name] {
		return bindToken{}, false, "unknown pipe \"" + name + "\" in {{" + inner + "}}"
	}
	tok = bindToken{Path: path, Pipe: name}
	if hasArg {
		tok.Arg = strings.TrimSpace(argPart)
	}
	return tok, true, ""
}

// matchPipedBindToken recognises a whole trimmed run of the shape
// {{ident(.ident)*}}, optionally carrying a single formatting pipe
// (S:S8.7). Used by <verbatim value="{{...}}"/>, the one other pipe-legal
// context besides a text run (S:S6.5) -- parseArgTokens deliberately keeps
// using the pipe-blind matchBindToken above instead, since args="..." is
// not a pipe-legal context.
func matchPipedBindToken(s string) (tok bindToken, ok bool, pipeErr string) {
	if len(s) < 4 || !strings.HasPrefix(s, "{{") || !strings.HasSuffix(s, "}}") {
		return bindToken{}, false, ""
	}
	inner := strings.TrimSpace(s[2 : len(s)-2])
	return parseBindBody(inner)
}

// scanBindToken tries to read one {{path}} (or {{path|pipe}},
// S:S8.7) token beginning at run[i], where run[i:i+2] is "{{". On success
// it returns the parsed token and the index just past the closing "}}";
// on failure -- no closing "}}", or a path half that is not a valid path
// with no "|" involved -- ok is false and the caller treats the "{{" as
// literal text (S:S8.4). pipeErr is a non-empty reason when the token does
// contain a "|" but its pipe half is malformed or names an unrecognised
// pipe; the caller always reports this as a loud parse error rather than
// falling through to literal text -- see parseBindBody. The first "}}"
// closes the token, so one token never spans another.
func scanBindToken(run string, i int) (tok bindToken, end int, ok bool, pipeErr string) {
	rest := run[i+2:]
	j := strings.Index(rest, "}}")
	if j < 0 {
		return bindToken{}, 0, false, ""
	}
	inner := strings.TrimSpace(rest[:j])
	end = i + 2 + j + 2
	tok, ok, pipeErr = parseBindBody(inner)
	return tok, end, ok, pipeErr
}

func isValidPath(s string) bool {
	if s == "" {
		return false
	}
	for step := range strings.SplitSeq(s, ".") {
		if !isIdent(step) {
			return false
		}
	}
	return true
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		letter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
		digit := c >= '0' && c <= '9'
		if i == 0 && !letter {
			return false
		}
		if i > 0 && !letter && !digit {
			return false
		}
	}
	return true
}
