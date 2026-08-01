//go:build auditdocs

package php

func ExampleNewHandler_stub() {
	_ = NewHandler
}

func ExampleHandler_LaravelRoot_stub() {
	_ = (*Handler).LaravelRoot
}

func ExampleHandler_DocRoot_stub() {
	_ = (*Handler).DocRoot
}

func ExampleHandler_ServeHTTP_stub() {
	_ = (*Handler).ServeHTTP
}
