//go:build auditdocs

package php

func ExampleNewHandler() {
	_ = NewHandler
}

func ExampleHandler_LaravelRoot() {
	_ = (*Handler).LaravelRoot
}

func ExampleHandler_DocRoot() {
	_ = (*Handler).DocRoot
}

func ExampleHandler_ServeHTTP() {
	_ = (*Handler).ServeHTTP
}
