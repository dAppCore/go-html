package display

import (
	"context"
	"reflect"
	"slices"

	core "dappco.re/go"
	"dappco.re/go/render/display/webkit/pkg/deno"
	"dappco.re/go/render/display/webkit/pkg/internal/coreutil"
)

func (s *Service) registerSidecarActions() {
	if core.Trim(core.Env("CORE_DENO_ENABLE")) != "" && s.sidecar == nil {
		manager, err := s.sidecarForStart()
		if err != nil {
			if s != nil && s.ServiceRuntime != nil && s.Core() != nil {
				s.Core().LogWarn(err, "display.registerSidecarActions", "skipping sidecar auto-start; invalid sidecar environment")
			}
			s.sidecar = nil
		} else if binary := core.Trim(manager.Status().Binary); binary != "" {
			if _, err := lookPath(binary); err != nil {
				if s != nil && s.ServiceRuntime != nil && s.Core() != nil {
					s.Core().LogWarn(err, "display.registerSidecarActions", "skipping sidecar auto-start; binary unavailable")
				}
				s.sidecar = nil
			} else if _, err := manager.Start(context.Background()); err != nil {
				if s != nil && s.ServiceRuntime != nil && s.Core() != nil {
					s.Core().LogError(err, "display.registerSidecarActions", "failed to start enabled sidecar")
				}
				s.sidecar = nil
			}
		}
	}

	s.Core().Action("display.sidecar.start", func(ctx context.Context, _ core.Options) core.Result {
		status, err := s.startSidecar(ctx)
		return core.Result{}.New(status, err)
	})
	s.Core().Action("display.sidecar.stop", func(ctx context.Context, _ core.Options) core.Result {
		manager := s.ensureSidecar()
		status, err := manager.Stop(ctx)
		return core.Result{}.New(status, err)
	})
	s.Core().Action("display.sidecar.status", func(_ context.Context, _ core.Options) core.Result {
		return core.Result{Value: s.ensureSidecar().Status(), OK: true}
	})
	s.Core().Action("core.deno.sidecar.start", func(ctx context.Context, _ core.Options) core.Result {
		status, err := s.startSidecar(ctx)
		return core.Result{}.New(status, err)
	})
	s.Core().Action("core.deno.sidecar.stop", func(ctx context.Context, _ core.Options) core.Result {
		status, err := s.ensureSidecar().Stop(ctx)
		return core.Result{}.New(status, err)
	})
	s.Core().Action("core.deno.sidecar.status", func(_ context.Context, _ core.Options) core.Result {
		return core.Result{Value: s.ensureSidecar().Status(), OK: true}
	})
	s.Core().Action("display.sidecar.eval", func(ctx context.Context, opts core.Options) core.Result {
		result, err := s.ensureSidecar().Eval(ctx, opts.String("code"))
		return core.Result{}.New(result, err)
	})
	s.Core().Action("core.deno.sidecar.eval", func(ctx context.Context, opts core.Options) core.Result {
		result, err := s.ensureSidecar().Eval(ctx, opts.String("code"))
		return core.Result{}.New(result, err)
	})
}

func (s *Service) ensureSidecar() *deno.Manager {
	if s.sidecar == nil {
		s.sidecar = s.newSidecar(deno.Options{
			Binary: core.Trim(core.Env("CORE_DENO_BINARY")),
			Dir:    core.Trim(core.Env("CORE_DENO_DIR")),
			Args:   splitCommandArgs(core.Env("CORE_DENO_ARGS")),
			Core:   s.coreRef(),
		})
	}
	return s.sidecar
}

func (s *Service) startSidecar(ctx context.Context) (deno.Status, resultFailure) {
	manager, err := s.sidecarForStart()
	if err != nil {
		s.sidecar = nil
		return deno.Status{}, err
	}
	return manager.Start(ctx)
}

func (s *Service) sidecarForStart() (*deno.Manager, resultFailure) {
	options, err := sidecarLaunchOptions(s.coreRef())
	if err != nil {
		return nil, err
	}
	if s.sidecar == nil || !s.sidecar.Status().Running {
		s.sidecar = s.newSidecar(options)
	}
	return s.sidecar, nil
}

func (s *Service) coreRef() *core.Core {
	if s != nil && s.ServiceRuntime != nil {
		return s.Core()
	}
	return nil
}

func (s *Service) newSidecar(options deno.Options) *deno.Manager {
	manager := deno.New(options)
	manager.OnEvent(func(event deno.Event) {
		if s == nil || s.events == nil {
			return
		}
		s.events.Emit(Event{
			Type: EventCustomEvent,
			Data: map[string]any{
				"source": "deno",
				"name":   event.Name,
				"data":   event.Data,
			},
		})
	})
	return manager
}

func sidecarLaunchOptions(coreRef *core.Core) (deno.Options, resultFailure) {
	args := splitCommandArgs(core.Env("CORE_DENO_ARGS"))
	if err := validateSidecarArgs(args, coreRef); err != nil {
		return deno.Options{}, err
	}

	binary, err := validateSidecarBinary(core.Env("CORE_DENO_BINARY"))
	if err != nil {
		return deno.Options{}, err
	}
	dir, err := validateSidecarDir(core.Env("CORE_DENO_DIR"))
	if err != nil {
		return deno.Options{}, err
	}

	return deno.Options{
		Binary: binary,
		Dir:    dir,
		Args:   args,
		Core:   coreRef,
	}, nil
}

func validateSidecarArgs(args []string, coreRef *core.Core) resultFailure {
	for _, arg := range args {
		flag := denoPermissionFlag(arg)
		if flag == "" {
			continue
		}
		msg := core.Sprintf("CORE_DENO_ARGS contains permission flag %s; deno sandbox is being weakened. This is intentional only if you understand the implications.", flag)
		if equalFold(core.Trim(core.Env("CORE_DENO_ALLOW_PERMISSIONS")), "true") {
			core.Print(sidecarWarningWriter, "%s", msg)
			if coreRef != nil {
				coreRef.LogWarn(core.Errorf("permission flag %s", flag), "display.sidecar.env", msg)
			}
			continue
		}
		return core.Errorf("%s Set CORE_DENO_ALLOW_PERMISSIONS=true to allow this intentionally", msg)
	}
	return nil
}

func denoPermissionFlag(arg string) string {
	token := core.Trim(arg)
	switch {
	case token == "-A" || core.HasPrefix(token, "-A="):
		return "-A"
	case token == "--allow-all" || core.HasPrefix(token, "--allow-all="):
		return "--allow-all"
	case core.HasPrefix(token, "--allow-"):
		if flag, _, ok := cut(token, "="); ok {
			return flag
		}
		return token
	default:
		return ""
	}
}

func validateSidecarBinary(value string) (string, resultFailure) {
	binary := core.Trim(value)
	if binary == "" {
		return "", nil
	}
	if !core.PathIsAbs(binary) {
		return "", core.Errorf("CORE_DENO_BINARY must be an absolute path: %q", binary)
	}
	info, err := coreStat(binary)
	if err != nil {
		return "", core.Errorf("CORE_DENO_BINARY does not exist: %q: %w", binary, err)
	}
	if info.IsDir() {
		return "", core.Errorf("CORE_DENO_BINARY must point to a file, not a directory: %q", binary)
	}
	if core.PathBase(binary) != "deno" {
		return "", core.Errorf("CORE_DENO_BINARY must point to a binary named deno: %q", binary)
	}
	resolved, err := pathEvalSymlinks(binary)
	if err != nil {
		return "", core.Errorf("CORE_DENO_BINARY could not be resolved: %q: %w", binary, err)
	}
	if core.PathBase(resolved) != "deno" {
		return "", core.Errorf("CORE_DENO_BINARY must resolve to a binary named deno: %q", binary)
	}
	return resolved, nil
}

func validateSidecarDir(value string) (string, resultFailure) {
	dir := core.Trim(value)
	if dir == "" {
		return "", nil
	}
	if hasParentPathComponent(dir) {
		return "", core.Errorf("CORE_DENO_DIR must not contain .. path components: %q", dir)
	}
	abs, err := pathAbs(dir)
	if err != nil {
		return "", core.Errorf("CORE_DENO_DIR could not be made absolute: %q: %w", dir, err)
	}
	if err := rejectSymlinkPathComponents(abs); err != nil {
		return "", err
	}
	info, err := coreStat(abs)
	if err != nil {
		return "", core.Errorf("CORE_DENO_DIR does not exist: %q: %w", dir, err)
	}
	if !info.IsDir() {
		return "", core.Errorf("CORE_DENO_DIR must be an existing directory: %q", dir)
	}
	return abs, nil
}

func hasParentPathComponent(path string) bool {
	return slices.Contains(core.Split(core.PathToSlash(path), "/"), "..")
}

func rejectSymlinkPathComponents(path string) resultFailure {
	clean := core.CleanPath(path, string(core.PathSeparator))
	volume := pathVolumeName(clean)
	rest := core.TrimPrefix(clean, volume)
	current := volume
	if core.HasPrefix(rest, string(core.PathSeparator)) {
		current += string(core.PathSeparator)
		rest = core.TrimPrefix(rest, string(core.PathSeparator))
	}
	for _, part := range core.Split(rest, string(core.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		current = core.PathJoin(current, part)
		info, err := coreLstat(current)
		if err != nil {
			return core.Errorf("CORE_DENO_DIR component does not exist: %q: %w", current, err)
		}
		if info.Mode()&core.ModeSymlink != 0 {
			return core.Errorf("CORE_DENO_DIR must not contain symlink component: %q", current)
		}
	}
	return nil
}

func splitCommandArgs(value string) []string {
	fields := fields(core.Trim(value))
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func (s *Service) forwardIPCToSidecar(msg core.Message) {
	if s == nil || s.sidecar == nil {
		return
	}
	status := s.sidecar.Status()
	if !status.Running || !status.Connected {
		return
	}
	typeName := ""
	if t := reflect.TypeOf(msg); t != nil {
		typeName = t.String()
	}
	if err := s.sidecar.Emit("core.ipc.message", map[string]any{
		"type": typeName,
		"data": normalizeSidecarValue(msg),
	}); err != nil {
		coreutil.LogWarn(s.Core(), err, "display.emitSidecarIPC", "sidecar emit failed")
	}
}

func normalizeSidecarValue(value any) any {
	if value == nil {
		return nil
	}
	var normalized any
	if result := core.JSONUnmarshalString(core.JSONMarshalString(value), &normalized); result.OK {
		return normalized
	}
	return map[string]any{"value": core.JSONMarshalString(value)}
}
