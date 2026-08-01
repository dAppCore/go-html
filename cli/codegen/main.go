//go:build !js

// Package main provides a build-time CLI for generating Web Component bundles.
// Reads a JSON slot map from stdin, writes the generated JS or TypeScript to stdout.
//
// Usage:
//
//	echo '{"H":"nav-bar","C":"main-content"}' | go run ./cmd/codegen/ > components.js
//	echo '{"H":"nav-bar","C":"main-content"}' | go run ./cmd/codegen/ -types > components.d.ts
//	go run ./cmd/codegen/ -watch --input=slots.json --output=components.js
package main

import (
	"context"
	"time"

	core "dappco.re/go"
	coreio "dappco.re/go/io"
	"dappco.re/go/render/engine/codegen"
)

const defaultPollInterval = 250 * time.Millisecond
const defaultInputPath = "/dev/stdin"
const defaultOutputPath = "/dev/stdout"

func generate(data []byte, emitTypes bool) core.Result {
	var slots map[string]string
	if result := core.JSONUnmarshal(data, &slots); !result.OK {
		err, _ := result.Value.(error)
		return core.Fail(core.E("codegen", "invalid JSON", err))
	}

	if emitTypes {
		return core.Ok(codegen.GenerateTypeScriptDefinitions(slots))
	}

	outResult := codegen.GenerateBundle(slots)
	if !outResult.OK {
		return resultError("codegen", "generate bundle", outResult)
	}
	return outResult
}

func run(input, output any, emitTypes bool) core.Result {
	dataResult := readInput(input)
	if !dataResult.OK {
		return resultError("codegen", "reading input", dataResult)
	}
	data, _ := dataResult.Value.([]byte)

	outResult := generate(data, emitTypes)
	if !outResult.OK {
		return outResult
	}
	out, _ := outResult.Value.(string)

	writeResult := writeOutput(output, out)
	if !writeResult.OK {
		return resultError("codegen", "writing output", writeResult)
	}
	return core.Ok(nil)
}

func readInput(input any) core.Result {
	if path, ok := input.(string); ok {
		return readLocalFile(path)
	}

	result := core.ReadAll(input)
	if !result.OK {
		return resultError("codegen", "reading input stream", result)
	}
	content, _ := result.Value.(string)
	return core.Ok([]byte(content))
}

func writeOutput(output any, content string) core.Result {
	if path, ok := output.(string); ok {
		return writeLocalFile(path, content)
	}

	result := core.WriteAll(output, content)
	if !result.OK {
		return resultError("codegen", "writing output stream", result)
	}
	return core.Ok(nil)
}

func runDaemon(ctx context.Context, inputPath, outputPath string, emitTypes bool, pollInterval time.Duration) core.Result {
	if inputPath == "" {
		return core.Fail(core.E("codegen", "watch mode requires -input", nil))
	}
	if outputPath == "" {
		return core.Fail(core.E("codegen", "watch mode requires -output", nil))
	}
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}

	var lastInput []byte
	for {
		inputResult := readLocalFile(inputPath)
		if !inputResult.OK {
			return resultError("codegen", "reading input file", inputResult)
		}
		input, _ := inputResult.Value.([]byte)

		if !sameBytes(input, lastInput) {
			outResult := generate(input, emitTypes)
			if !outResult.OK {
				return outResult
			}
			out, _ := outResult.Value.(string)
			writeResult := writeLocalFile(outputPath, out)
			if !writeResult.OK {
				return resultError("codegen", "writing output file", writeResult)
			}
			lastInput = append(lastInput[:0], input...)
		}

		select {
		case <-ctx.Done():
			if core.Is(ctx.Err(), context.Canceled) {
				return core.Ok(nil)
			}
			return core.Fail(ctx.Err())
		case <-time.After(pollInterval):
		}
	}
}

func readLocalFile(path string) core.Result {
	if path == "" {
		path = defaultInputPath
	}

	if path == defaultInputPath {
		f, err := coreio.Local.Open(path)
		if err != nil {
			return core.Fail(err)
		}

		result := core.ReadAll(f)
		if !result.OK {
			return resultError("codegen", "reading stdin", result)
		}
		content, _ := result.Value.(string)
		return core.Ok([]byte(content))
	}

	content, err := coreio.Local.Read(path)
	if err != nil {
		return core.Fail(err)
	}
	return core.Ok([]byte(content))
}

func writeLocalFile(path, content string) core.Result {
	if path == "" {
		path = defaultOutputPath
	}

	if path == defaultOutputPath {
		f, err := coreio.Local.Create(path)
		if err != nil {
			f, err = coreio.Local.Append(path)
			if err != nil {
				core.Print(nil, "%s", content)
				return core.Ok(nil)
			}
		}

		result := core.WriteAll(f, content)
		if !result.OK {
			return resultError("codegen", "writing stdout", result)
		}
		return core.Ok(nil)
	}

	return resultFromError(coreio.Local.Write(path, content))
}

func sameBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range len(a) {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func runStdio(emitTypes bool) core.Result {
	return run(defaultInputPath, defaultOutputPath, emitTypes)
}

func newCodegenApp() *core.Core {
	// core.WithCli() is required from dappco.re/go v0.12.0 onward — the CLI
	// command framework became an opt-in service instead of a default one,
	// and codegen's -watch/-types dispatch (registerCodegenCommands below)
	// depends on it being present.
	c := core.New(core.WithOption("name", "codegen"), core.WithCli())
	if cli := c.Cli(); cli != nil {
		cli.SetOutput(core.Discard)
	}

	registerCodegenCommands(c)
	return c
}

func registerCodegenCommands(c *core.Core) {
	c.Command("generate", core.Command{
		Description: "Generate JavaScript or TypeScript from a JSON slot map on stdin",
		Flags:       codegenCommandFlags(),
		Action: func(opts core.Options) core.Result {
			return runGenerateCommand(opts, opts.Bool("types"))
		},
	})
	c.Command("types", core.Command{
		Description: "Generate TypeScript declarations from a JSON slot map on stdin",
		Flags:       codegenCommandFlags(),
		Action: func(opts core.Options) core.Result {
			return runGenerateCommand(opts, true)
		},
	})
	c.Command("-types", core.Command{
		Hidden: true,
		Flags:  codegenCommandFlags(),
		Action: func(opts core.Options) core.Result {
			return runGenerateCommand(opts, true)
		},
	})
	c.Command("--types", core.Command{
		Hidden: true,
		Flags:  codegenCommandFlags(),
		Action: func(opts core.Options) core.Result {
			return runGenerateCommand(opts, true)
		},
	})
	c.Command("watch", core.Command{
		Description: "Poll an input JSON file and rewrite the generated output",
		Flags:       codegenCommandFlags(),
		Action: func(opts core.Options) core.Result {
			return runWatchCommand(c, opts, opts.Bool("types"))
		},
	})
	c.Command("-watch", core.Command{
		Hidden: true,
		Flags:  codegenCommandFlags(),
		Action: func(opts core.Options) core.Result {
			return runWatchCommand(c, opts, opts.Bool("types"))
		},
	})
	c.Command("--watch", core.Command{
		Hidden: true,
		Flags:  codegenCommandFlags(),
		Action: func(opts core.Options) core.Result {
			return runWatchCommand(c, opts, opts.Bool("types"))
		},
	})
}

func codegenCommandFlags() core.Options {
	return core.NewOptions(
		core.Option{Key: "types", Value: false},
		core.Option{Key: "input", Value: ""},
		core.Option{Key: "output", Value: ""},
		core.Option{Key: "poll", Value: defaultPollInterval.String()},
	)
}

func runGenerateCommand(opts core.Options, emitTypes bool) core.Result {
	return run(inputPathFromOptions(opts), outputPathFromOptions(opts), emitTypes)
}

func inputPathFromOptions(opts core.Options) string {
	if input := opts.String("input"); input != "" {
		return input
	}
	return defaultInputPath
}

func outputPathFromOptions(opts core.Options) string {
	if output := opts.String("output"); output != "" {
		return output
	}
	return defaultOutputPath
}

func runWatchCommand(c *core.Core, opts core.Options, emitTypes bool) core.Result {
	pollResult := pollIntervalFromOptions(opts)
	if !pollResult.OK {
		return pollResult
	}
	pollInterval, _ := pollResult.Value.(time.Duration)

	ctx := context.Background()
	if c != nil {
		ctx = c.Context()
	}
	return runDaemon(ctx, opts.String("input"), opts.String("output"), emitTypes, pollInterval)
}

func pollIntervalFromOptions(opts core.Options) core.Result {
	raw := opts.String("poll")
	if raw == "" {
		return core.Ok(defaultPollInterval)
	}

	pollInterval, err := time.ParseDuration(raw)
	if err != nil {
		return core.Fail(core.E("codegen", "invalid poll interval", err))
	}
	return core.Ok(pollInterval)
}

func runCodegenApp(c *core.Core) core.Result {
	if c == nil {
		return core.Fail(core.E("codegen.main", "core app is required", nil))
	}

	defer func() {
		if result := c.ServiceShutdown(context.Background()); !result.OK {
			core.Warn("codegen shutdown failed", "scope", "codegen.main", "err", result.Error())
		}
	}()

	if result := c.ServiceStartup(c.Context(), nil); !result.OK {
		return resultError("codegen.main", "startup failed", result)
	}

	cli := c.Cli()
	if cli == nil {
		return runStdio(false)
	}

	result := cli.Run()
	if result.OK {
		return core.Ok(nil)
	}
	if err, ok := result.Value.(error); ok && err != nil {
		return core.Fail(err)
	}

	return runStdio(false)
}

func resultFromError(err error) core.Result {
	if err != nil {
		return core.Fail(err)
	}
	return core.Ok(nil)
}

func resultError(op, msg string, result core.Result) core.Result {
	if result.OK {
		return core.Ok(nil)
	}
	if err, ok := result.Value.(error); ok && err != nil {
		return core.Fail(core.E(op, msg, err))
	}
	return core.Fail(core.E(op, msg, nil))
}

func main() {
	c := newCodegenApp()
	if result := runCodegenApp(c); !result.OK {
		core.Error("codegen failed", "scope", "codegen.main", "err", result.Error())
	}
}
