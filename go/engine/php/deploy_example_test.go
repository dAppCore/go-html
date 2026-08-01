//go:build auditdocs

package php

func ExampleDeploy() {
	_ = Deploy
}

func ExampleDeployStatus() {
	_ = DeployStatus
}

func ExampleRollback() {
	_ = Rollback
}

func ExampleListDeployments() {
	_ = ListDeployments
}

func ExampleIsDeploymentComplete() {
	_ = IsDeploymentComplete
}

func ExampleIsDeploymentSuccessful() {
	_ = IsDeploymentSuccessful
}
