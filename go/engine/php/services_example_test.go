//go:build auditdocs

package php

func ExampleService_Name() {
	_ = (*baseService).Name
}

func ExampleService_Status() {
	_ = (*baseService).Status
}

func ExampleService_Logs() {
	_ = (*baseService).Logs
}

func ExampleNewFrankenPHPService() {
	_ = NewFrankenPHPService
}

func ExampleFrankenPHPService_Start() {
	_ = (*FrankenPHPService).Start
}

func ExampleFrankenPHPService_Stop() {
	_ = (*FrankenPHPService).Stop
}

func ExampleNewViteService() {
	_ = NewViteService
}

func ExampleViteService_Start() {
	_ = (*ViteService).Start
}

func ExampleViteService_Stop() {
	_ = (*ViteService).Stop
}

func ExampleNewHorizonService() {
	_ = NewHorizonService
}

func ExampleHorizonService_Start() {
	_ = (*HorizonService).Start
}

func ExampleHorizonService_Stop() {
	_ = (*HorizonService).Stop
}

func ExampleNewReverbService() {
	_ = NewReverbService
}

func ExampleReverbService_Start() {
	_ = (*ReverbService).Start
}

func ExampleReverbService_Stop() {
	_ = (*ReverbService).Stop
}

func ExampleNewRedisService() {
	_ = NewRedisService
}

func ExampleRedisService_Start() {
	_ = (*RedisService).Start
}

func ExampleRedisService_Stop() {
	_ = (*RedisService).Stop
}

func ExampleReader_Read() {
	_ = (*tailReader).Read
}

func ExampleReader_Close() {
	_ = (*tailReader).Close
}
