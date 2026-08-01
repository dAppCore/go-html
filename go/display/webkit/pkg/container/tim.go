package container

import (
	"context"
	"sort"
	"sync"
	"time"

	core "dappco.re/go"
)

type TIMOptions struct {
	Name      string
	Image     string
	Command   []string
	WorkDir   string
	Env       map[string]string
	DataDir   string
	Runtime   ContainerRuntime
	Detect    func() ContainerRuntime
	Exec      func(context.Context, string, ...string) resultFailure
	Now       func() time.Time
	Resources TIMResources
}

type TIMResources struct {
	CPUCores int    `json:"cpu_cores,omitempty"`
	MemoryMB int    `json:"memory_mb,omitempty"`
	GPU      string `json:"gpu,omitempty"`
}

type TIMState struct {
	Name      string           `json:"name"`
	Image     string           `json:"image"`
	Runtime   ContainerRuntime `json:"runtime"`
	Status    string           `json:"status"`
	StartedAt time.Time        `json:"started_at"`
	Command   []string         `json:"command,omitempty"`
	DataDir   string           `json:"data_dir,omitempty"`
	Resources TIMResources     `json:"resources"`
}

type TIMManager struct {
	options TIMOptions
	mu      sync.Mutex
	state   TIMState
}

func NewTIMManager(options TIMOptions) *TIMManager {
	if core.Trim(options.Name) == "" {
		options.Name = "coregui-tim"
	}
	if core.Trim(options.Image) == "" {
		options.Image = "ghcr.io/lthn/core/tim:latest"
	}
	if options.Detect == nil {
		options.Detect = Detect
	}
	if options.Exec == nil {
		options.Exec = func(ctx context.Context, name string, args ...string) resultFailure {
			cmd := commandContext(ctx, name, args...)
			return cmd.Run()
		}
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &TIMManager{
		options: options,
		state: TIMState{
			Name:      options.Name,
			Image:     options.Image,
			Runtime:   coalesceRuntime(options.Runtime, options.Detect()),
			Status:    "stopped",
			Command:   append([]string(nil), options.Command...),
			DataDir:   options.DataDir,
			Resources: options.Resources,
		},
	}
}

func (m *TIMManager) State() TIMState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneTIMState(m.state)
}

func (m *TIMManager) Start(ctx context.Context) (TIMState, resultFailure) {
	m.mu.Lock()
	runtime := coalesceRuntime(m.options.Runtime, m.options.Detect())
	m.state.Runtime = runtime
	if runtime == RuntimeNone {
		state := cloneTIMState(m.state)
		m.mu.Unlock()
		return state, core.E("container.TIMManager.Start", "no supported container runtime detected", nil)
	}

	command, args := m.runtimeCommand(runtime, "run")
	m.mu.Unlock()

	if err := m.options.Exec(ctx, command, args...); err != nil {
		m.mu.Lock()
		m.state.Status = "error"
		state := cloneTIMState(m.state)
		m.mu.Unlock()
		return state, core.E("container.TIMManager.Start", "failed to execute runtime start command", err)
	}

	m.mu.Lock()
	m.state.Status = "running"
	m.state.StartedAt = m.options.Now()
	state := cloneTIMState(m.state)
	m.mu.Unlock()
	return state, nil
}

func (m *TIMManager) Stop(ctx context.Context) (TIMState, resultFailure) {
	m.mu.Lock()
	if m.state.Runtime == RuntimeNone {
		m.state.Status = "stopped"
		m.state.StartedAt = time.Time{}
		state := cloneTIMState(m.state)
		m.mu.Unlock()
		return state, nil
	}
	command, args := m.runtimeCommand(m.state.Runtime, "stop")
	m.mu.Unlock()

	if err := m.options.Exec(ctx, command, args...); err != nil {
		m.mu.Lock()
		m.state.Status = "error"
		state := cloneTIMState(m.state)
		m.mu.Unlock()
		return state, core.E("container.TIMManager.Stop", "failed to execute runtime stop command", err)
	}

	m.mu.Lock()
	m.state.Status = "stopped"
	m.state.StartedAt = time.Time{}
	state := cloneTIMState(m.state)
	m.mu.Unlock()
	return state, nil
}

func (m *TIMManager) runtimeCommand(runtime ContainerRuntime, verb string) (string, []string) {
	name := m.options.Name
	image := m.options.Image
	switch runtime {
	case RuntimeApple:
		if verb == "run" {
			args := append([]string{"run", "--name", name}, m.containerRunArgs()...)
			args = append(args, image)
			args = append(args, m.options.Command...)
			return "container", args
		}
		return "container", []string{"stop", name}
	case RuntimePodman:
		if verb == "run" {
			args := []string{"run", "-d", "--replace", "--name", name}
			args = append(args, resourceArgs(m.options.Resources)...)
			args = append(args, m.containerRunArgs()...)
			args = append(args, image)
			args = append(args, m.options.Command...)
			return "podman", args
		}
		return "podman", []string{"stop", name}
	default:
		if verb == "run" {
			args := []string{"run", "-d", "--rm", "--name", name}
			args = append(args, resourceArgs(m.options.Resources)...)
			args = append(args, m.containerRunArgs()...)
			args = append(args, image)
			args = append(args, m.options.Command...)
			return "docker", args
		}
		return "docker", []string{"stop", name}
	}
}

func (m *TIMManager) containerRunArgs() []string {
	var args []string
	if dataDir := core.Trim(m.options.DataDir); dataDir != "" {
		args = append(args, "-v", dataDir+":"+dataDir)
	}
	if workDir := core.Trim(m.options.WorkDir); workDir != "" {
		args = append(args, "-w", workDir)
	}
	env := make(map[string]string, len(m.options.Env))
	envKeys := make([]string, 0, len(m.options.Env))
	for key, value := range m.options.Env {
		trimmedKey := core.Trim(key)
		if trimmedKey != "" {
			env[trimmedKey] = value
			envKeys = append(envKeys, trimmedKey)
		}
	}
	sort.Strings(envKeys)
	for _, key := range envKeys {
		args = append(args, "-e", key+"="+env[key])
	}
	return args
}

func resourceArgs(resources TIMResources) []string {
	var args []string
	if resources.CPUCores > 0 {
		args = append(args, "--cpus", core.Sprintf("%d", resources.CPUCores))
	}
	if resources.MemoryMB > 0 {
		args = append(args, "--memory", core.Sprintf("%dm", resources.MemoryMB))
	}
	if core.Trim(resources.GPU) != "" {
		args = append(args, "--gpus", resources.GPU)
	}
	return args
}

func coalesceRuntime(values ...ContainerRuntime) ContainerRuntime {
	for _, value := range values {
		if value != RuntimeNone {
			return value
		}
	}
	return RuntimeNone
}

func cloneTIMState(state TIMState) TIMState {
	state.Command = append([]string(nil), state.Command...)
	return state
}
