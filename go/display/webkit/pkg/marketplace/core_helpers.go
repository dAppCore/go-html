package marketplace

import (
	"context"

	core "dappco.re/go"
)

func coreResultError(result core.Result, fallback string) resultFailure {
	if result.OK {
		return nil
	}
	if err, ok := result.Value.(error); ok {
		return err
	}
	if text := core.Trim(result.Error()); text != "" {
		return core.NewError(text)
	}
	return core.NewError(fallback)
}

func coreMkdirAll(path string, mode core.FileMode) resultFailure {
	return coreResultError(core.MkdirAll(path, mode), "failed to create directory")
}

func coreRemoveAll(path string) resultFailure {
	return coreResultError(core.RemoveAll(path), "failed to remove path")
}

func coreWriteFile(path string, data []byte, mode core.FileMode) resultFailure {
	return coreResultError(core.WriteFile(path, data, mode), "failed to write file")
}

func coreReadFile(path string) ([]byte, resultFailure) {
	result := core.ReadFile(path)
	if !result.OK {
		return nil, coreResultError(result, "failed to read file")
	}
	return result.Value.([]byte), nil
}

func coreStat(path string) (core.FsFileInfo, resultFailure) {
	result := core.Stat(path)
	if !result.OK {
		return nil, coreResultError(result, "failed to stat path")
	}
	return result.Value.(core.FsFileInfo), nil
}

func pathAbs(path string) (string, resultFailure) {
	result := core.PathAbs(path)
	if !result.OK {
		return "", coreResultError(result, "failed to make path absolute")
	}
	return result.Value.(string), nil
}

func pathEvalSymlinks(path string) (string, resultFailure) {
	result := core.PathEvalSymlinks(path)
	if !result.OK {
		return "", coreResultError(result, "failed to resolve symlinks")
	}
	return result.Value.(string), nil
}

func pathRel(base, target string) (string, resultFailure) {
	result := core.PathRel(base, target)
	if !result.OK {
		return "", coreResultError(result, "failed to compare paths")
	}
	return result.Value.(string), nil
}

func jsonUnmarshal(data []byte, target any) resultFailure {
	return coreResultError(core.JSONUnmarshal(data, target), "failed to decode JSON")
}

func commandContext(ctx context.Context, binary string, args ...string) *core.Cmd {
	cmd := &core.Cmd{Path: binary, Args: append([]string{binary}, args...)}
	if ctx != nil {
		go func() {
			<-ctx.Done()
			if cmd.Process != nil {
				if err := cmd.Process.Kill(); err != nil {
					core.Error("failed to kill marketplace command", "err", err)
				}
			}
		}()
	}
	return cmd
}

func containsAny(value, chars string) bool {
	for _, char := range chars {
		if core.Contains(value, string(char)) {
			return true
		}
	}
	return false
}

func repeatString(value string, count int) string {
	if count <= 0 || value == "" {
		return ""
	}
	builder := core.NewBuilder()
	for range count {
		builder.WriteString(value)
	}
	return builder.String()
}

func trimRunes(value, cutset string) string {
	for value != "" {
		trimmed := false
		for _, char := range cutset {
			part := string(char)
			if core.HasPrefix(value, part) {
				value = core.TrimPrefix(value, part)
				trimmed = true
			}
			if core.HasSuffix(value, part) {
				value = core.TrimSuffix(value, part)
				trimmed = true
			}
		}
		if !trimmed {
			return value
		}
	}
	return value
}
