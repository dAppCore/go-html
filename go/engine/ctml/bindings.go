// SPDX-Licence-Identifier: EUPL-1.2

package ctml

// Bindings supplies the host-side data a parsed .ctml tree needs at parse
// time: the row sequences for every <each items="name"> in the document
// (Sequences), and the document-wide scalar values a lone {{path}} token
// outside any <each> body resolves against (Values). A name absent from
// either map is data absence, not a parse defect -- an absent Sequences
// entry renders as an empty list and an absent Values key renders as empty
// text, so a .ctml file parses standalone before its data is wired up.
// docs/ctml.md S:S8 explains why the source has to be supplied at parse
// time rather than through Context at render time.
//
//	Usage example: ctml.Bindings{
//		Sequences: map[string][]map[string]any{
//			"repos": {{"name": "go-html", "status": "green"}},
//		},
//		Values: map[string]any{"user": "ada", "count": 3},
//	}
type Bindings struct {
	Sequences map[string][]map[string]any
	Values    map[string]any
}
