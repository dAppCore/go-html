package window

import core "dappco.re/go"

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

func coreReadFile(path string) ([]byte, resultFailure) {
	result := core.ReadFile(path)
	if !result.OK {
		return nil, coreResultError(result, "failed to read file")
	}
	return result.Value.([]byte), nil
}

func coreWriteFile(path string, data []byte, mode core.FileMode) resultFailure {
	return coreResultError(core.WriteFile(path, data, mode), "failed to write file")
}

// coreWriteFileAtomic publishes data at path by writing a uniquely-named
// sibling temp file and renaming it into place. Rename is atomic on POSIX,
// so a reader — or a concurrent writer racing this one — always observes a
// COMPLETE document. The plain truncate-and-write it replaces let two
// processes sharing one state file (lthn and core/ide both persist into
// the Core config directory) splice a short document onto the tail of a
// longer one. The suffix must be unique per writer: a fixed ".tmp" name
// would simply move the same splice into the temp file.
func coreWriteFileAtomic(path string, data []byte, mode core.FileMode) resultFailure {
	tmp := core.Concat(path, ".tmp.", core.Itoa(core.RandIntn(1<<30)))
	if err := coreResultError(core.WriteFile(tmp, data, mode), "failed to write temp file"); err != nil {
		return err
	}
	if err := coreResultError(core.Rename(tmp, path), "failed to publish file"); err != nil {
		core.Remove(tmp)
		return err
	}
	return nil
}

func coreMkdirAll(path string, mode core.FileMode) resultFailure {
	return coreResultError(core.MkdirAll(path, mode), "failed to create directory")
}

func equalFold(left, right string) bool {
	return core.Lower(left) == core.Lower(right)
}
