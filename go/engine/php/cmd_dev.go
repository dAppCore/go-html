package php

import (
	"bufio"
	"context"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	core "dappco.re/go"
	"dappco.re/go/cli/pkg/cli"
)

func addPHPDevCommand(c *core.Core, prefix string) {
	path := phpCommandPath(prefix, "dev")
	phpFailureorCommand(c, path, phpT("cmd.php.dev.short"), func(opts core.Options) error {
		line := phpCommandLineFor(path, opts)
		return runPHPDev(phpDevOptions{
			NoVite:    line.Bool("no-vite"),
			NoHorizon: line.Bool("no-horizon"),
			NoReverb:  line.Bool("no-reverb"),
			NoRedis:   line.Bool("no-redis"),
			HTTPS:     line.Bool("https"),
			Domain:    line.String("domain", ""),
			Port:      line.Int("port", 0),
		})
	})
}

type phpDevOptions struct {
	NoVite    bool
	NoHorizon bool
	NoReverb  bool
	NoRedis   bool
	HTTPS     bool
	Domain    string
	Port      int
}

func runPHPDev(opts phpDevOptions) error { // Result boundary
	cwdResult := core.Getwd()
	if !cwdResult.OK {
		return core.E("php", "failed to get working directory", cwdResult.Value.(error))
	}
	cwd := cwdResult.Value.(string)

	// Check if this is a Laravel project
	if !IsLaravelProject(cwd) {
		return core.E("php", phpT("cmd.php.error.not_laravel"), nil)
	}

	cli.Print(cliLabelValueBlankFormat, dimStyle.Render(phpT(cmdPHPLabelKey)), phpT("cmd.php.dev.starting", map[string]any{"AppName": laravelDisplayName(cwd)}))

	// Detect services
	services := DetectServices(cwd)
	printDetectedServices(services)

	// Create and start dev server
	devOpts := makeDevServerOptions(cwd, opts)
	server := NewDevServer(devOpts)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	notifyDevShutdown(cancel)

	if err := server.Start(ctx, devOpts); err != nil {
		return core.E("php", phpT("i18n.fail.start", "services"), err)
	}

	// Print status
	printDevServerReady(cwd, opts, devOpts.FrankenPHPPort, services, server)

	cli.Print("\n%s\n\n", dimStyle.Render(phpT("cmd.php.dev.press_ctrl_c")))

	// Stream unified logs
	streamDevLogs(ctx, server)

	// Stop services
	if err := server.Stop(); err != nil {
		cli.Print(cliLabelValueFormat, errorStyle.Render(phpLabel("error")), phpT("cmd.php.dev.stop_error", map[string]any{"Error": err}))
	}

	cli.Print(cliLabelValueFormat, successStyle.Render(phpLabel("done")), phpT("cmd.php.dev.all_stopped"))
	return nil
}
func laravelDisplayName(dir string) string {
	appName := GetLaravelAppName(dir)
	if appName == "" {
		return "Laravel"
	}
	return appName
}

func printDetectedServices(services []DetectedService) {
	cli.Print(cliLabelValueFormat, dimStyle.Render(phpT("cmd.php.label.services")), phpT("cmd.php.dev.detected_services"))
	for _, svc := range services {
		cli.Print(cliIndentedLabelValueFormat, successStyle.Render("*"), svc)
	}
	cli.Blank()
}

func makeDevServerOptions(cwd string, opts phpDevOptions) Options {
	port := opts.Port
	if port == 0 {
		port = 8000
	}

	return Options{
		Dir:            cwd,
		NoVite:         opts.NoVite,
		NoHorizon:      opts.NoHorizon,
		NoReverb:       opts.NoReverb,
		NoRedis:        opts.NoRedis,
		HTTPS:          opts.HTTPS,
		Domain:         opts.Domain,
		FrankenPHPPort: port,
	}
}

func notifyDevShutdown(cancel context.CancelFunc) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		cli.Print(cliSectionLabelValueFormat, dimStyle.Render(phpT(cmdPHPLabelKey)), phpT("cmd.php.dev.shutting_down"))
		cancel()
	}()
}

func printDevServerReady(cwd string, opts phpDevOptions, port int, services []DetectedService, server *DevServer) {
	cli.Print(cliLabelValueFormat, successStyle.Render(phpT("cmd.php.label.running")), phpT("cmd.php.dev.services_started"))
	printServiceStatuses(server.Status())
	cli.Blank()
	cli.Print(cliLabelValueFormat, dimStyle.Render(phpT("cmd.php.label.app_url")), linkStyle.Render(devAppURL(cwd, opts, port)))

	if !opts.NoVite && containsService(services, ServiceVite) {
		cli.Print(cliLabelValueFormat, dimStyle.Render(phpT("cmd.php.label.vite")), linkStyle.Render("http://localhost:5173"))
	}
}

func devAppURL(cwd string, opts phpDevOptions, port int) string {
	appURL := GetLaravelAppURL(cwd)
	if appURL != "" {
		return appURL
	}
	if opts.HTTPS {
		return cli.Sprintf("https://localhost:%d", port)
	}
	return cli.Sprintf("http://localhost:%d", port)
}

func streamDevLogs(ctx context.Context, server *DevServer) {
	logsReader, err := server.Logs("", true)
	if err != nil {
		cli.Print(cliLabelValueFormat, errorStyle.Render(phpLabel("warning")), phpT(i18nFailGetKey, "logs"))
		return
	}
	defer func() { _ = logsReader.Close() }()

	scanner := bufio.NewScanner(logsReader)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
			printColoredLog(scanner.Text())
		}
	}
}

func addPHPLogsCommand(c *core.Core, prefix string) {
	path := phpCommandPath(prefix, "logs")
	phpFailureorCommand(c, path, phpT("cmd.php.logs.short"), func(opts core.Options) error {
		line := phpCommandLineFor(path, opts)
		return runPHPLogs(line.String("service", ""), line.Bool("follow"))
	})
}

func runPHPLogs(service string, follow bool) error { // Result boundary
	cwdResult := core.Getwd()
	if !cwdResult.OK {
		return cwdResult.Value.(error)
	}
	cwd := cwdResult.Value.(string)

	if !IsLaravelProject(cwd) {
		return core.E("php", phpT("cmd.php.error.not_laravel_short"), nil)
	}

	// Create a minimal server just to access logs
	server := NewDevServer(Options{Dir: cwd})

	logsReader, err := server.Logs(service, follow)
	if err != nil {
		return core.E("php", phpT(i18nFailGetKey, "logs"), err)
	}
	defer func() { _ = logsReader.Close() }()

	// Handle interrupt
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		cancel()
	}()

	scanner := bufio.NewScanner(logsReader)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
			printColoredLog(scanner.Text())
		}
	}

	return scanner.Err()
}

func addPHPStopCommand(c *core.Core, prefix string) {
	phpFailureorCommand(c, phpCommandPath(prefix, "stop"), phpT("cmd.php.stop.short"), func(core.Options) error {
		return runPHPStop()
	})
}

func runPHPStop() error { // Result boundary
	cwdResult := core.Getwd()
	if !cwdResult.OK {
		return cwdResult.Value.(error)
	}
	cwd := cwdResult.Value.(string)

	cli.Print(cliLabelValueFormat, dimStyle.Render(phpT(cmdPHPLabelKey)), phpT("cmd.php.stop.stopping"))

	// We need to find running processes
	// This is a simplified version - in practice you'd want to track PIDs
	server := NewDevServer(Options{Dir: cwd})
	if err := server.Stop(); err != nil {
		return core.E("php", phpT("i18n.fail.stop", "services"), err)
	}

	cli.Print(cliLabelValueFormat, successStyle.Render(phpLabel("done")), phpT("cmd.php.dev.all_stopped"))
	return nil
}

func addPHPStatusCommand(c *core.Core, prefix string) {
	phpFailureorCommand(c, phpCommandPath(prefix, "status"), phpT("cmd.php.status.short"), func(core.Options) error {
		return runPHPStatus()
	})
}

func runPHPStatus() error { // Result boundary
	cwdResult := core.Getwd()
	if !cwdResult.OK {
		return cwdResult.Value.(error)
	}
	cwd := cwdResult.Value.(string)

	if !IsLaravelProject(cwd) {
		return core.E("php", phpT("cmd.php.error.not_laravel_short"), nil)
	}

	appName := GetLaravelAppName(cwd)
	if appName == "" {
		appName = "Laravel"
	}

	cli.Print(cliLabelValueBlankFormat, dimStyle.Render(phpLabel("project")), appName)

	// Detect available services
	services := DetectServices(cwd)
	cli.Print("%s\n", dimStyle.Render(phpT("cmd.php.status.detected_services")))
	for _, svc := range services {
		style := getServiceStyle(string(svc))
		cli.Print(cliIndentedLabelValueFormat, style.Render("*"), svc)
	}
	cli.Blank()

	// Package manager
	pm := DetectPackageManager(cwd)
	cli.Print(cliLabelValueFormat, dimStyle.Render(phpT("cmd.php.status.package_manager")), pm)

	// FrankenPHP status
	if IsFrankenPHPProject(cwd) {
		cli.Print(cliLabelValueFormat, dimStyle.Render(phpT("cmd.php.status.octane_server")), "FrankenPHP")
	}

	// SSL status
	appURL := GetLaravelAppURL(cwd)
	if appURL != "" {
		domain := ExtractDomainFromURL(appURL)
		if CertsExist(domain, SSLOptions{}) {
			cli.Print(cliLabelValueFormat, dimStyle.Render(phpT("cmd.php.status.ssl_certs")), successStyle.Render(phpT("cmd.php.status.ssl_installed")))
		} else {
			cli.Print(cliLabelValueFormat, dimStyle.Render(phpT("cmd.php.status.ssl_certs")), dimStyle.Render(phpT("cmd.php.status.ssl_not_setup")))
		}
	}

	return nil
}

func addPHPSSLCommand(c *core.Core, prefix string) {
	path := phpCommandPath(prefix, "ssl")
	phpFailureorCommand(c, path, phpT("cmd.php.ssl.short"), func(opts core.Options) error {
		line := phpCommandLineFor(path, opts)
		return runPHPSSL(line.String("domain", ""))
	})
}

func runPHPSSL(domain string) error { // Result boundary
	cwdResult := core.Getwd()
	if !cwdResult.OK {
		return cwdResult.Value.(error)
	}
	cwd := cwdResult.Value.(string)

	// Get domain from APP_URL if not specified
	if domain == "" {
		appURL := GetLaravelAppURL(cwd)
		if appURL != "" {
			domain = ExtractDomainFromURL(appURL)
		}
	}
	if domain == "" {
		domain = "localhost"
	}

	// Check if mkcert is installed
	if !IsMkcertInstalled() {
		cli.Print(cliLabelValueFormat, errorStyle.Render(phpLabel("error")), phpT("cmd.php.ssl.mkcert_not_installed"))
		cli.Print(cliSingleLineFormat, phpT("common.hint.install_with"))
		cli.Print("  %s\n", phpT("cmd.php.ssl.install_macos"))
		cli.Print("  %s\n", phpT("cmd.php.ssl.install_linux"))
		return core.E("php", phpT("cmd.php.error.mkcert_not_installed"), nil)
	}

	cli.Print(cliLabelValueFormat, dimStyle.Render("SSL:"), phpT("cmd.php.ssl.setting_up", map[string]any{"Domain": domain}))

	// Check if certs already exist
	if CertsExist(domain, SSLOptions{}) {
		cli.Print(cliLabelValueFormat, dimStyle.Render(phpLabel("skip")), phpT("cmd.php.ssl.certs_exist"))

		certFile, keyFile, _ := CertPaths(domain, SSLOptions{})
		cli.Print(cliLabelValueFormat, dimStyle.Render(phpT("cmd.php.ssl.cert_label")), certFile)
		cli.Print(cliLabelValueFormat, dimStyle.Render(phpT("cmd.php.ssl.key_label")), keyFile)
		return nil
	}

	// Setup SSL
	if err := SetupSSL(domain, SSLOptions{}); err != nil {
		return core.E("php", phpT("i18n.fail.setup", "SSL"), err)
	}

	certFile, keyFile, _ := CertPaths(domain, SSLOptions{})

	cli.Print(cliLabelValueFormat, successStyle.Render(phpLabel("done")), phpT("cmd.php.ssl.certs_created"))
	cli.Print(cliLabelValueFormat, dimStyle.Render(phpT("cmd.php.ssl.cert_label")), certFile)
	cli.Print(cliLabelValueFormat, dimStyle.Render(phpT("cmd.php.ssl.key_label")), keyFile)

	return nil
}

// Helper functions for dev commands

func printServiceStatuses(statuses []ServiceStatus) {
	for _, s := range statuses {
		style := getServiceStyle(s.Name)
		var statusText string

		if s.Error != nil {
			statusText = phpStatusError.Render(phpT("cmd.php.status.error", map[string]any{"Error": s.Error}))
		} else if s.Running {
			statusText = phpStatusRunning.Render(phpT("cmd.php.status.running"))
			if s.Port > 0 {
				statusText += dimStyle.Render(cli.Sprintf(" (%s)", phpT("cmd.php.status.port", map[string]any{"Port": s.Port})))
			}
			if s.PID > 0 {
				statusText += dimStyle.Render(cli.Sprintf(" [%s]", phpT("cmd.php.status.pid", map[string]any{"PID": s.PID})))
			}
		} else {
			statusText = phpStatusStopped.Render(phpT("cmd.php.status.stopped"))
		}

		cli.Print(cliIndentedLabelValueFormat, style.Render(s.Name+":"), statusText)
	}
}

func printColoredLog(line string) {
	// Parse service prefix from log line
	timestamp := time.Now().Format("15:04:05")

	var style *cli.AnsiStyle
	serviceName := ""

	if core.HasPrefix(line, "[FrankenPHP]") {
		style = phpFrankenPHPStyle
		serviceName = "FrankenPHP"
		line = core.TrimPrefix(line, "[FrankenPHP] ")
	} else if core.HasPrefix(line, "[Vite]") {
		style = phpViteStyle
		serviceName = "Vite"
		line = core.TrimPrefix(line, "[Vite] ")
	} else if core.HasPrefix(line, "[Horizon]") {
		style = phpHorizonStyle
		serviceName = "Horizon"
		line = core.TrimPrefix(line, "[Horizon] ")
	} else if core.HasPrefix(line, "[Reverb]") {
		style = phpReverbStyle
		serviceName = "Reverb"
		line = core.TrimPrefix(line, "[Reverb] ")
	} else if core.HasPrefix(line, "[Redis]") {
		style = phpRedisStyle
		serviceName = "Redis"
		line = core.TrimPrefix(line, "[Redis] ")
	} else {
		// Unknown service, print as-is
		cli.Print(cliLabelValueFormat, dimStyle.Render(timestamp), line)
		return
	}

	cli.Print("%s %s %s\n",
		dimStyle.Render(timestamp),
		style.Render(cli.Sprintf("[%s]", serviceName)),
		line,
	)
}

func getServiceStyle(name string) *cli.AnsiStyle {
	switch core.Lower(name) {
	case "frankenphp":
		return phpFrankenPHPStyle
	case "vite":
		return phpViteStyle
	case "horizon":
		return phpHorizonStyle
	case "reverb":
		return phpReverbStyle
	case "redis":
		return phpRedisStyle
	default:
		return dimStyle
	}
}

func containsService(services []DetectedService, target DetectedService) bool {
	return slices.Contains(services, target)
}
