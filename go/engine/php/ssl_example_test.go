//go:build auditdocs

package php

func ExampleGetSSLDir() {
	_ = GetSSLDir
}

func ExampleCertPaths() {
	_ = CertPaths
}

func ExampleCertsExist() {
	_ = CertsExist
}

func ExampleSetupSSL() {
	_ = SetupSSL
}

func ExampleSetupSSLIfNeeded() {
	_ = SetupSSLIfNeeded
}

func ExampleIsMkcertInstalled() {
	_ = IsMkcertInstalled
}

func ExampleInstallMkcertCA() {
	_ = InstallMkcertCA
}

func ExampleGetMkcertCARoot() {
	_ = GetMkcertCARoot
}
