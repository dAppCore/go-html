//go:build auditdocs

package php

func ExampleGenerateDockerfile() {
	_ = GenerateDockerfile
}

func ExampleDetectDockerfileConfig() {
	_ = DetectDockerfileConfig
}

func ExampleGenerateDockerfileFromConfig() {
	_ = GenerateDockerfileFromConfig
}

func ExampleGenerateDockerignore() {
	_ = GenerateDockerignore
}
