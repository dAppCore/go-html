//go:build auditdocs

package php

func ExampleIsLaravelProject() {
	_ = IsLaravelProject
}

func ExampleIsFrankenPHPProject() {
	_ = IsFrankenPHPProject
}

func ExampleDetectServices() {
	_ = DetectServices
}

func ExampleDetectPackageManager() {
	_ = DetectPackageManager
}

func ExampleGetLaravelAppName() {
	_ = GetLaravelAppName
}

func ExampleGetLaravelAppURL() {
	_ = GetLaravelAppURL
}

func ExampleExtractDomainFromURL() {
	_ = ExtractDomainFromURL
}
