//go:build auditdocs

package php

func ExampleResponseWriter_Header() {
	_ = (*execResponseWriter).Header
}

func ExampleResponseWriter_Write() {
	_ = (*execResponseWriter).Write
}

func ExampleResponseWriter_WriteHeader() {
	_ = (*execResponseWriter).WriteHeader
}
