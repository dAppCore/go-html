package chat

import (
	"maps"
	"sync"

	core "dappco.re/go"
)

type chatStore struct {
	path string
	mu   sync.Mutex
	data map[string]map[string]string
}

func newChatStore(path string) (*chatStore, resultFailure) {
	store := &chatStore{
		path: path,
		data: make(map[string]map[string]string),
	}
	if core.Trim(path) == "" || path == ":memory:" {
		return store, nil
	}
	if result := core.MkdirAll(core.PathDir(path), 0o755); !result.OK {
		return nil, coreResultError(result, "failed to create chat store directory")
	}
	content, err := coreReadFile(path)
	if err != nil {
		if core.IsNotExist(err) {
			return store, nil
		}
		return nil, err
	}
	if len(content) == 0 {
		return store, nil
	}
	if result := core.JSONUnmarshal(content, &store.data); !result.OK {
		return nil, coreResultError(result, "failed to decode chat store")
	}
	return store, nil
}

func (s *chatStore) set(group, key, value string) resultFailure {
	if s == nil {
		return core.NewError("chat store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = make(map[string]map[string]string)
	}
	items := s.data[group]
	if items == nil {
		items = make(map[string]string)
		s.data[group] = items
	}
	items[key] = value
	return s.persistLocked()
}

func (s *chatStore) get(group, key string) (string, resultFailure) {
	if s == nil {
		return "", core.NewError("chat store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if value, ok := s.data[group][key]; ok {
		return value, nil
	}
	return "", core.NewError("not found")
}

func (s *chatStore) getAll(group string) (map[string]string, resultFailure) {
	if s == nil {
		return nil, core.NewError("chat store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := make(map[string]string)
	maps.Copy(copy, s.data[group])
	return copy, nil
}

func (s *chatStore) delete(group, key string) resultFailure {
	if s == nil {
		return core.NewError("chat store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if items := s.data[group]; items != nil {
		delete(items, key)
		if len(items) == 0 {
			delete(s.data, group)
		}
	}
	return s.persistLocked()
}

func (s *chatStore) persistLocked() resultFailure {
	if s.path == "" || s.path == ":memory:" {
		return nil
	}
	result := core.JSONMarshalIndent(s.data, "", "  ")
	if !result.OK {
		return coreResultError(result, "failed to encode chat store")
	}
	return coreWriteFile(s.path, result.Value.([]byte), 0o644)
}
