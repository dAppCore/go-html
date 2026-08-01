//go:build auditdocs

package php

func ExampleDetectFormatter() {
	_ = DetectFormatter
}

func ExampleDetectAnalyser() {
	_ = DetectAnalyser
}

func ExampleFormat() {
	_ = Format
}

func ExampleAnalyse() {
	_ = Analyse
}

func ExampleDetectPsalm() {
	_ = DetectPsalm
}

func ExampleRunPsalm() {
	_ = RunPsalm
}

func ExampleRunAudit() {
	_ = RunAudit
}

func ExampleDetectRector() {
	_ = DetectRector
}

func ExampleRunRector() {
	_ = RunRector
}

func ExampleDetectInfection() {
	_ = DetectInfection
}

func ExampleRunInfection() {
	_ = RunInfection
}

func ExampleGetQAStages() {
	_ = GetQAStages
}

func ExampleGetQAChecks() {
	_ = GetQAChecks
}

func ExampleRunSecurityChecks() {
	_ = RunSecurityChecks
}
