//go:build auditdocs

package php

func ExampleNewBridge() {
	_ = NewBridge
}

func ExampleBridge_Port() {
	_ = (*Bridge).Port
}

func ExampleBridge_URL() {
	_ = (*Bridge).URL
}

func ExampleBridge_Shutdown() {
	_ = (*Bridge).Shutdown
}
