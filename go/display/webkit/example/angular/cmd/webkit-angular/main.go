// SPDX-Licence-Identifier: EUPL-1.2

// Command webkit-angular runs the in-tree Angular + display/webkit
// example.
//
//	# built bundle (run `npm install && npm run build` in ui/ first)
//	go run ./display/webkit/example/angular/cmd/webkit-angular
//
//	# live dev server (run `npm start` in ui/ in another shell)
//	WEBKIT_ANGULAR_DEV=1 go run ./display/webkit/example/angular/cmd/webkit-angular
//
// Set WEBKIT_ANGULAR_STATE to a directory to keep window state and
// layouts out of the shared default — the exact mitigation for the
// corruption two apps sharing one window_state.json used to cause.
package main

import (
	"time"

	core "dappco.re/go"
	webkit "dappco.re/go/render/display/webkit"
	"dappco.re/go/render/display/webkit/example/angular"
)

// TickEvent is the event name the renderer subscribes to. Namespacing it
// per-app matters: the bus is global, so two features using "tick" get
// each other's payloads.
const TickEvent = "webkit-angular:tick"

func main() {
	opts := angular.HostOptions{
		Dev:      core.Env("WEBKIT_ANGULAR_DEV") != "",
		StateDir: core.Env("WEBKIT_ANGULAR_STATE"),
	}

	cfg, err := angular.Config(opts)
	if err != nil {
		panic(err)
	}

	c := core.New(core.WithName("gui", webkit.NewService(cfg)))
	service, ok := core.ServiceFor[*webkit.Service](c, "gui")
	if !ok || service == nil {
		panic(core.E("webkit-angular", "gui service is unavailable", nil))
	}

	ctx := core.Background()
	if result := service.OnStartup(ctx); !result.OK {
		panic(core.E("webkit-angular", "start gui service", result.Err()))
	}
	if !webkit.OpenWindow(c, "main") {
		panic(core.E("webkit-angular", "open the main window", nil))
	}

	// Go → renderer events. The Angular side subscribes with
	// Events.On(TickEvent, …); see ui/src/app/wails.service.ts.
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-ticker.C:
				webkit.EmitEvent(c, TickEvent, now.UTC().Format(time.RFC3339))
			}
		}
	}()

	runResult := service.Run()
	close(stop)
	if !runResult.OK {
		panic(core.E("webkit-angular", "run gui service", runResult.Err()))
	}
}
