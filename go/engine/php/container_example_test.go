//go:build auditdocs

package php

func ExampleBuildDocker() {
	_ = BuildDocker
}

func ExampleBuildLinuxKit() {
	_ = BuildLinuxKit
}

func ExampleServeProduction() {
	_ = ServeProduction
}

func ExampleShell() {
	_ = Shell
}

func ExampleIsPHPProject() {
	_ = IsPHPProject
}
