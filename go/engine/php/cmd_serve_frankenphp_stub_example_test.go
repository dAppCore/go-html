//go:build auditdocs

package php

func ExampleResponseWriter_Header_stub() {
	_ = (*execResponseWriter).Header
}

func ExampleResponseWriter_Write_stub() {
	_ = (*execResponseWriter).Write
}

func ExampleResponseWriter_WriteHeader_stub() {
	_ = (*execResponseWriter).WriteHeader
}
