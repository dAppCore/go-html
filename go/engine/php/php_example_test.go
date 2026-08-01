//go:build auditdocs

package php

func ExampleNewDevServer() {
	_ = NewDevServer
}

func ExampleDevServer_Start() {
	_ = (*DevServer).Start
}

func ExampleDevServer_Stop() {
	_ = (*DevServer).Stop
}

func ExampleDevServer_Logs() {
	_ = (*DevServer).Logs
}

func ExampleDevServer_Status() {
	_ = (*DevServer).Status
}

func ExampleDevServer_IsRunning() {
	_ = (*DevServer).IsRunning
}

func ExampleDevServer_Services() {
	_ = (*DevServer).Services
}

func ExampleServiceReader_Read() {
	_ = (*multiServiceReader).Read
}

func ExampleServiceReader_Close() {
	_ = (*multiServiceReader).Close
}
