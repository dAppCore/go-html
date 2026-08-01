//go:build auditdocs

package php

func ExampleCommandLine_Bool() {
	_ = phpCommandLine.Bool
}

func ExampleCommandLine_String() {
	_ = phpCommandLine.String
}

func ExampleCommandLine_Int() {
	_ = phpCommandLine.Int
}

func ExampleCommandLine_Args() {
	_ = phpCommandLine.Args
}
