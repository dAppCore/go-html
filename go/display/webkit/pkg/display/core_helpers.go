package display

import (
	"unicode"

	core "dappco.re/go"
)

var sidecarWarningWriter core.Writer = core.Stderr()

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

func coreMkdir(path string, mode core.FileMode) resultFailure {
	return coreResultError(core.Mkdir(path, mode), "failed to create directory")
}

func coreRemoveAll(path string) resultFailure {
	return coreResultError(core.RemoveAll(path), "failed to remove path")
}

func coreWriteFile(path string, data []byte, mode core.FileMode) resultFailure {
	return coreResultError(core.WriteFile(path, data, mode), "failed to write file")
}

func coreWriteMode(path, content string, mode core.FileMode) resultFailure {
	return coreWriteFile(path, []byte(content), mode)
}

func coreEnsureDir(path string) resultFailure {
	return coreMkdirAll(path, 0o755)
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

func coreLstat(path string) (core.FsFileInfo, resultFailure) {
	result := core.Lstat(path)
	if !result.OK {
		return nil, coreResultError(result, "failed to stat path")
	}
	return result.Value.(core.FsFileInfo), nil
}

func coreHostname() (string, resultFailure) {
	result := core.Hostname()
	if !result.OK {
		return "", coreResultError(result, "failed to read hostname")
	}
	return result.Value.(string), nil
}

func coreUserConfigDir() (string, resultFailure) {
	result := core.UserConfigDir()
	if !result.OK {
		return "", coreResultError(result, "failed to read config dir")
	}
	return result.Value.(string), nil
}

func coreUserHomeDir() (string, resultFailure) {
	result := core.UserHomeDir()
	if !result.OK {
		return "", coreResultError(result, "failed to read home dir")
	}
	return result.Value.(string), nil
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

func pathVolumeName(path string) string {
	if len(path) >= 2 && path[1] == ':' {
		return path[:2]
	}
	if core.HasPrefix(path, `\\`) {
		parts := core.Split(path, `\`)
		if len(parts) >= 4 {
			return core.Join(`\`, parts[:4]...)
		}
	}
	return ""
}

func pathFromSlash(path string) string {
	if core.PathSeparator == '/' {
		return path
	}
	return core.Replace(path, "/", string(core.PathSeparator))
}

func jsonMarshal(value any) ([]byte, resultFailure) {
	result := core.JSONMarshal(value)
	if !result.OK {
		return nil, coreResultError(result, "failed to encode JSON")
	}
	return result.Value.([]byte), nil
}

func jsonUnmarshal(data []byte, target any) resultFailure {
	return coreResultError(core.JSONUnmarshal(data, target), "failed to decode JSON")
}

func bytesEqual(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func equalFold(left, right string) bool {
	return core.Lower(left) == core.Lower(right)
}

func cut(value, sep string) (string, string, bool) {
	parts := core.SplitN(value, sep, 2)
	if len(parts) != 2 {
		return value, "", false
	}
	return parts[0], parts[1], true
}

func fields(value string) []string {
	var result []string
	start := -1
	for index, char := range value {
		if unicode.IsSpace(char) {
			if start >= 0 {
				result = append(result, value[start:index])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = index
		}
	}
	if start >= 0 {
		result = append(result, value[start:])
	}
	return result
}

func indexString(value, needle string) int {
	if needle == "" {
		return 0
	}
	if len(needle) > len(value) {
		return -1
	}
	for index := 0; index <= len(value)-len(needle); index++ {
		if value[index:index+len(needle)] == needle {
			return index
		}
	}
	return -1
}

func lookPath(file string) (string, resultFailure) {
	name := core.Trim(file)
	if name == "" {
		return "", core.NewError("executable name is empty")
	}
	if core.Contains(name, string(core.PathSeparator)) || core.PathIsAbs(name) {
		if executablePath(name) {
			return name, nil
		}
		return "", core.Errorf("executable file not found in path: %s", file)
	}
	for _, dir := range core.Split(core.Getenv("PATH"), string(core.PathListSeparator)) {
		if core.Trim(dir) == "" {
			continue
		}
		candidate := core.PathJoin(dir, name)
		if executablePath(candidate) {
			return candidate, nil
		}
	}
	return "", core.Errorf("executable file not found in path: %s", file)
}

func executablePath(path string) bool {
	result := core.Stat(path)
	if !result.OK {
		return false
	}
	info := result.Value.(core.FsFileInfo)
	return !info.IsDir() && info.Mode().Perm()&0o111 != 0
}

func trimRight(value, cutset string) string {
	for value != "" {
		trimmed := false
		for _, char := range cutset {
			part := string(char)
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
