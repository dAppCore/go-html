//go:build auditdocs

package php

func ExampleNewCoolifyClient() {
	_ = NewCoolifyClient
}

func ExampleLoadCoolifyConfig() {
	_ = LoadCoolifyConfig
}

func ExampleLoadCoolifyConfigFromFile() {
	_ = LoadCoolifyConfigFromFile
}

func ExampleCoolifyClient_TriggerDeploy() {
	_ = (*CoolifyClient).TriggerDeploy
}

func ExampleCoolifyClient_GetDeployment() {
	_ = (*CoolifyClient).GetDeployment
}

func ExampleCoolifyClient_ListDeployments() {
	_ = (*CoolifyClient).ListDeployments
}

func ExampleCoolifyClient_Rollback() {
	_ = (*CoolifyClient).Rollback
}

func ExampleCoolifyClient_GetApp() {
	_ = (*CoolifyClient).GetApp
}
