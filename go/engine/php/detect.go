package php

import (
	core "dappco.re/go"
)

// DetectedService represents a service that was detected in a Laravel project.
type DetectedService string

// Detected service constants for Laravel projects.
const (
	// ServiceFrankenPHP indicates FrankenPHP server is detected.
	ServiceFrankenPHP DetectedService = "frankenphp"
	// ServiceVite indicates Vite frontend bundler is detected.
	ServiceVite DetectedService = "vite"
	// ServiceHorizon indicates Laravel Horizon queue dashboard is detected.
	ServiceHorizon DetectedService = "horizon"
	// ServiceReverb indicates Laravel Reverb WebSocket server is detected.
	ServiceReverb DetectedService = "reverb"
	// ServiceRedis indicates Redis cache/queue backend is detected.
	ServiceRedis DetectedService = "redis"
)

// IsLaravelProject checks if the given directory is a Laravel project.
// It looks for the presence of artisan file and laravel in composer.json.
func IsLaravelProject(dir string) bool {

	// Check for artisan file
	artisanPath := core.PathJoin(dir, "artisan")
	if !probeExists(artisanPath) {
		return false
	}

	// Check composer.json for laravel/framework
	composerPath := core.PathJoin(dir, composerJSONFile)
	data, ok := probeRead(composerPath)
	if !ok {
		return false
	}

	var composer struct {
		Require    map[string]string `json:"require"`
		RequireDev map[string]string `json:"require-dev"`
	}

	if r := core.JSONUnmarshal([]byte(data), &composer); !r.OK {
		return false
	}

	// Check for laravel/framework in require
	if _, ok := composer.Require["laravel/framework"]; ok {
		return true
	}

	// Also check require-dev (less common but possible)
	if _, ok := composer.RequireDev["laravel/framework"]; ok {
		return true
	}

	return false
}

// IsFrankenPHPProject checks if the project is configured for FrankenPHP.
// It looks for laravel/octane with frankenphp driver.
func IsFrankenPHPProject(dir string) bool {

	// Check composer.json for laravel/octane
	composerPath := core.PathJoin(dir, composerJSONFile)
	data, ok := probeRead(composerPath)
	if !ok {
		return false
	}

	var composer struct {
		Require map[string]string `json:"require"`
	}

	if r := core.JSONUnmarshal([]byte(data), &composer); !r.OK {
		return false
	}

	if _, ok := composer.Require["laravel/octane"]; !ok {
		return false
	}

	// Check octane config for frankenphp
	configPath := core.PathJoin(dir, "config", "octane.php")
	if !probeExists(configPath) {
		// If no config exists but octane is installed, assume frankenphp
		return true
	}

	configData, ok := probeRead(configPath)
	if !ok {
		return true // Assume frankenphp if we can't read config
	}

	// Look for frankenphp in the config
	return core.Contains(configData, "frankenphp")
}

// DetectServices detects which services are needed based on project files.
func DetectServices(dir string) []DetectedService {
	services := []DetectedService{}

	// FrankenPHP/Octane is always needed for a Laravel dev environment
	if IsFrankenPHPProject(dir) || IsLaravelProject(dir) {
		services = append(services, ServiceFrankenPHP)
	}

	// Check for Vite
	if hasVite(dir) {
		services = append(services, ServiceVite)
	}

	// Check for Horizon
	if hasHorizon(dir) {
		services = append(services, ServiceHorizon)
	}

	// Check for Reverb
	if hasReverb(dir) {
		services = append(services, ServiceReverb)
	}

	// Check for Redis
	if needsRedis(dir) {
		services = append(services, ServiceRedis)
	}

	return services
}

// hasVite checks if the project uses Vite.
func hasVite(dir string) bool {
	viteConfigs := []string{
		"vite.config.js",
		"vite.config.ts",
		"vite.config.mjs",
		"vite.config.mts",
	}

	for _, config := range viteConfigs {
		if probeExists(core.PathJoin(dir, config)) {
			return true
		}
	}

	return false
}

// hasHorizon checks if Laravel Horizon is configured.
func hasHorizon(dir string) bool {
	horizonConfig := core.PathJoin(dir, "config", "horizon.php")
	return probeExists(horizonConfig)
}

// hasReverb checks if Laravel Reverb is configured.
func hasReverb(dir string) bool {
	reverbConfig := core.PathJoin(dir, "config", "reverb.php")
	return probeExists(reverbConfig)
}

// needsRedis checks if the project uses Redis based on .env configuration.
func needsRedis(dir string) bool {
	envPath := core.PathJoin(dir, ".env")
	content, ok := probeRead(envPath)
	if !ok {
		return false
	}

	lines := core.Split(content, "\n")
	for _, line := range lines {
		line = core.Trim(line)
		if core.HasPrefix(line, "#") {
			continue
		}

		// Check for Redis-related environment variables
		redisIndicators := []string{
			"REDIS_HOST=",
			"CACHE_DRIVER=redis",
			"QUEUE_CONNECTION=redis",
			"SESSION_DRIVER=redis",
			"BROADCAST_DRIVER=redis",
		}

		for _, indicator := range redisIndicators {
			if core.HasPrefix(line, indicator) {
				// Check if it's set to localhost or 127.0.0.1
				if core.Contains(line, "127.0.0.1") || core.Contains(line, "localhost") ||
					indicator != "REDIS_HOST=" {
					return true
				}
			}
		}
	}

	return false
}

// DetectPackageManager detects which package manager is used in the project.
// Returns "npm", "pnpm", "yarn", or "bun".
func DetectPackageManager(dir string) string {
	// Check for lock files in order of preference
	lockFiles := []struct {
		file    string
		manager string
	}{
		{"bun.lockb", "bun"},
		{"pnpm-lock.yaml", "pnpm"},
		{"yarn.lock", "yarn"},
		{"package-lock.json", "npm"},
	}

	for _, lf := range lockFiles {
		if probeExists(core.PathJoin(dir, lf.file)) {
			return lf.manager
		}
	}

	// Default to npm if no lock file found
	return "npm"
}

// GetLaravelAppName extracts the application name from Laravel's .env file.
func GetLaravelAppName(dir string) string {
	envPath := core.PathJoin(dir, ".env")
	content, ok := probeRead(envPath)
	if !ok {
		return ""
	}

	lines := core.Split(content, "\n")
	for _, line := range lines {
		line = core.Trim(line)
		if core.HasPrefix(line, "APP_NAME=") {
			value := core.TrimPrefix(line, "APP_NAME=")
			// Remove quotes if present
			value = trimQuotes(value)
			return value
		}
	}

	return ""
}

// GetLaravelAppURL extracts the application URL from Laravel's .env file.
func GetLaravelAppURL(dir string) string {
	envPath := core.PathJoin(dir, ".env")
	content, ok := probeRead(envPath)
	if !ok {
		return ""
	}

	lines := core.Split(content, "\n")
	for _, line := range lines {
		line = core.Trim(line)
		if core.HasPrefix(line, "APP_URL=") {
			value := core.TrimPrefix(line, "APP_URL=")
			// Remove quotes if present
			value = trimQuotes(value)
			return value
		}
	}

	return ""
}

// trimQuotes strips matching surrounding `"` or `'` characters from s. Equivalent
// of strings.Trim(s, `"'`) without importing strings; the cutset variant of
// core.Trim is not yet published in this repo's pinned core/go release.
func trimQuotes(s string) string {
	for len(s) > 0 && (s[0] == '"' || s[0] == '\'') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == '"' || s[len(s)-1] == '\'') {
		s = s[:len(s)-1]
	}
	return s
}

// ExtractDomainFromURL extracts the domain from a URL string.
func ExtractDomainFromURL(url string) string {
	// Remove protocol
	domain := core.TrimPrefix(url, "https://")
	domain = core.TrimPrefix(domain, "http://")

	// Remove port if present
	if parts := core.SplitN(domain, ":", 2); len(parts) > 1 {
		domain = parts[0]
	}

	// Remove path if present
	if parts := core.SplitN(domain, "/", 2); len(parts) > 1 {
		domain = parts[0]
	}

	return domain
}
