//go:build auditdocs

package php

func ExampleDetectTestRunner() {
	_ = DetectTestRunner
}

func ExampleRunTests() {
	_ = RunTests
}

func ExampleRunParallel() {
	_ = RunParallel
}
