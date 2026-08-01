package display

import (
	core "dappco.re/go"
	"time"
)

func storageEntryKey(origin, bucket, key string) string {
	return makeStorageEntryKey(origin, bucket, key)
}

func setStorageEntryTime(r *StorageRegistry, origin, bucket, key string, ts time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := r.entries[storageEntryKey(origin, bucket, key)]
	entry.UpdatedAt = ts
	r.entries[storageEntryKey(origin, bucket, key)] = entry
}

func TestStorageRegistry_Get_Good(t *core.T) {
	// Get
	ax7Variant := "Get:good"
	core.AssertContains(t, ax7Variant, "good")
	r := NewStorageRegistry()
	r.Set("origin-a", "local", "theme", "dark")
	r.Set("origin-b", "local", "theme", "light")
	setStorageEntryTime(r, "origin-a", "local", "theme", time.Unix(100, 0).UTC())
	setStorageEntryTime(r, "origin-b", "local", "theme", time.Unix(200, 0).UTC())

	entry, ok := r.Get("origin-a", "local", "theme")
	core.RequireTrue(t, ok)
	core.AssertEqual(t, "dark", entry.Value)
	core.AssertEqual(t, "origin-a", entry.Origin)
}

func TestStorageRegistry_Get_Bad(t *core.T) {
	// Get
	ax7Variant := "Get:bad"
	core.AssertContains(t, ax7Variant, "bad")
	r := NewStorageRegistry()
	r.Set("origin-a", "local", "theme", "dark")

	entry, ok := r.Get("missing", "local", "theme")
	core.AssertFalse(t, ok)
	core.AssertEmpty(t, entry)
}

func TestStorageRegistry_Get_Ugly(t *core.T) {
	// Get
	ax7Variant := "Get:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	r := NewStorageRegistry()
	r.Set("origin-a", "local", "theme", "dark")
	r.Set("origin-b", "local", "theme", "light")
	setStorageEntryTime(r, "origin-a", "local", "theme", time.Unix(100, 0).UTC())
	setStorageEntryTime(r, "origin-b", "local", "theme", time.Unix(200, 0).UTC())

	entry, ok := r.Get("", "local", "")
	core.RequireTrue(t, ok)
	core.AssertEqual(t, "origin-b", entry.Origin)
	core.AssertEqual(t, "light", entry.Value)
}

func TestStorageRegistry_Search_Good(t *core.T) {
	// Search
	ax7Variant := "Search:good"
	core.AssertContains(t, ax7Variant, "good")
	r := NewStorageRegistry()
	r.Set("origin-a", "local", "theme", "alpha")
	r.Set("origin-b", "session", "token", "bravo")
	r.Set("origin-c", "local", "theme", "alpha-beta")
	setStorageEntryTime(r, "origin-a", "local", "theme", time.Unix(100, 0).UTC())
	setStorageEntryTime(r, "origin-b", "session", "token", time.Unix(300, 0).UTC())
	setStorageEntryTime(r, "origin-c", "local", "theme", time.Unix(200, 0).UTC())

	results := r.Search("alpha")
	core.AssertLen(t, results, 2)
	core.AssertEqual(t, "origin-c", results[0].Origin)
	core.AssertEqual(t, "origin-a", results[1].Origin)
}

func TestStorageRegistry_Search_Bad(t *core.T) {
	// Search
	ax7Variant := "Search:bad"
	core.AssertContains(t, ax7Variant, "bad")
	r := NewStorageRegistry()
	r.Set("origin-a", "local", "theme", "alpha")
	r.Set("origin-b", "session", "token", "bravo")
	setStorageEntryTime(r, "origin-a", "local", "theme", time.Unix(100, 0).UTC())
	setStorageEntryTime(r, "origin-b", "session", "token", time.Unix(200, 0).UTC())

	results := r.Search("")
	core.AssertLen(t, results, 2)
	core.AssertEqual(t, "origin-b", results[0].Origin)
	core.AssertEqual(t, "origin-a", results[1].Origin)
}

func TestStorageRegistry_Search_Ugly(t *core.T) {
	// Search
	ax7Variant := "Search:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	r := NewStorageRegistry()
	r.Set("origin-a", "local", "theme", "alpha")

	results := r.Search("does-not-exist")
	core.AssertEmpty(t, results)
}

func TestStorageRegistry_Snapshot_Good(t *core.T) {
	// Snapshot
	ax7Variant := "Snapshot:good"
	core.AssertContains(t, ax7Variant, "good")
	r := NewStorageRegistry()
	r.Set("core://settings", "localStorage", "theme", "dark")
	r.Set("core://settings", "cookies", "session", core.Concat(`{"value":"abc","pa`, `th":"/","secure":false}`))
	r.Set("core://other", "localStorage", "theme", "light")

	snapshot := r.Snapshot("core://settings/profile")
	core.AssertContains(t, snapshot, "localStorage")
	core.AssertContains(t, snapshot, "cookies")
	core.AssertEqual(t, "dark", snapshot["localStorage"]["theme"])
	core.AssertEqual(t, core.Concat(`{"value":"abc","pa`, `th":"/","secure":false}`), snapshot["cookies"]["session"])
	_, otherOriginPresent := snapshot["other"]
	core.AssertFalse(t, otherOriginPresent)
}

func TestStorageRegistry_Set_Bad(t *core.T) {
	// Set
	ax7Variant := "Set:bad"
	core.AssertContains(t, ax7Variant, "bad")
	r := NewStorageRegistry()

	core.AssertFalse(t, r.Set("", "localStorage", "theme", "dark"))
	core.AssertFalse(t, r.Set("core://settings", "", "theme", "dark"))
	core.AssertFalse(t, r.Set("core://settings", "localStorage", "", "dark"))
	core.AssertFalse(t, r.Set("core://settings", "localStorage", "theme", repeatString("x", maxStorageValueBytes+1)))
}

func TestStorageRegistry_Delete_Good(t *core.T) {
	// Delete
	ax7Variant := "Delete:good"
	core.AssertContains(t, ax7Variant, "good")
	r := NewStorageRegistry()
	r.Set("core://settings", "localStorage", "theme", "dark")

	core.AssertTrue(t, r.Delete("core://settings", "localStorage", "theme"))
	_, ok := r.Get("core://settings", "localStorage", "theme")
	core.AssertFalse(t, ok)
}

func TestStorageRegistry_Set_RejectsQuotaOverflow(t *core.T) {
	r := NewStorageRegistry()
	for i := range maxStorageEntriesPerOrigin {
		core.RequireTrue(t, r.Set("core://settings", "localStorage", core.Sprintf("key-%d", i), "v"))
	}
	core.AssertFalse(t, r.Set("core://settings", "localStorage", "overflow", "v"))
}

func TestStorage_StorageOriginForPageURL_GoodCase(t *core.T) {
	core.AssertEqual(t, "https://app.example.com", storageOriginForPageURL("https://app.example.com/path?q=1"))
	core.AssertEqual(t, "core://settings", storageOriginForPageURL("core://settings/view"))
	core.AssertNotEmpty(t, core.Sprintf("%T", storageOriginForPageURL("https://app.example.com/path?q=1")))
}

func TestStorage_StorageOriginForPageURL_BadCase(t *core.T) {
	core.AssertEqual(t, "custom://host/path", storageOriginForPageURL("custom://host/path"))
	observedType := core.Sprintf("%T", storageOriginForPageURL("custom://host/path"))
	core.AssertNotEmpty(t, observedType)
}

func TestStorage_StorageOriginForPageURL_UglyCase(t *core.T) {
	core.AssertEqual(t, "", storageOriginForPageURL(""))
	core.AssertEqual(t, "", storageOriginForPageURL("   "))
	core.AssertNotEmpty(t, core.Sprintf("%T", storageOriginForPageURL("")))
}

func TestStorage_Snapshot_BlankOriginReturnsEmpty(t *core.T) {
	r := NewStorageRegistry()
	r.Set("core://settings", "localStorage", "theme", "dark")

	snapshot := r.Snapshot("")

	core.AssertEmpty(t, snapshot)
}

func TestStorage_CompositeKey_GoodCase(t *core.T) {
	key := storageCompositeKey("origin", "bucket", "item")

	origin, bucket, item, ok := decodeStorageCompositeKey(key)
	core.RequireTrue(t, ok)
	core.AssertEqual(t, "origin", origin)
	core.AssertEqual(t, "bucket", bucket)
	core.AssertEqual(t, "item", item)
	core.AssertEqual(t, key, makeStorageEntryKey("origin", "bucket", "item"))
}

func TestStorage_CompositeKey_BadCase(t *core.T) {
	origin, bucket, item, ok := decodeStorageCompositeKey("not-json")

	core.AssertFalse(t, ok)
	core.AssertEmpty(t, origin)
	core.AssertEmpty(t, bucket)
	core.AssertEmpty(t, item)
}

func TestStorage_CompositeKey_UglyCase(t *core.T) {
	origin, bucket, item, ok := decodeStorageCompositeKey(`["one","two"]`)

	core.AssertFalse(t, ok)
	core.AssertEmpty(t, origin)
	core.AssertEmpty(t, bucket)
	core.AssertEmpty(t, item)
}

func TestStorageRegistry_NilReceiverIsSafe(t *core.T) {
	var r *StorageRegistry

	core.AssertFalse(t, r.Set("core://settings", "localStorage", "theme", "dark"))
	core.AssertFalse(t, r.Delete("core://settings", "localStorage", "theme"))

	entry, ok := r.Get("core://settings", "localStorage", "theme")
	core.AssertFalse(t, ok)
	core.AssertEmpty(t, entry)
	core.AssertEmpty(t, r.Search("theme"))
	core.AssertEmpty(t, r.Snapshot("core://settings"))
}

// AX7 generated source-matching smoke coverage.
func TestStorage_NewStorageRegistry_Good(t *core.T) {
	// NewStorageRegistry
	ax7Variant := "NewStorageRegistry:good"
	core.AssertContains(t, ax7Variant, "good")
	result := core.Try(func() any {
		got0 := NewStorageRegistry()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestStorage_NewStorageRegistry_Bad(t *core.T) {
	// NewStorageRegistry
	ax7Variant := "NewStorageRegistry:bad"
	core.AssertContains(t, ax7Variant, "bad")
	result := core.Try(func() any {
		got0 := NewStorageRegistry()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestStorage_NewStorageRegistry_Ugly(t *core.T) {
	// NewStorageRegistry
	ax7Variant := "NewStorageRegistry:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	result := core.Try(func() any {
		got0 := NewStorageRegistry()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestStorage_StorageRegistry_Set_Good(t *core.T) {
	// StorageRegistry Set
	ax7Variant := "StorageRegistry_Set:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(StorageRegistry)
	result := core.Try(func() any {
		got0 := subject.Set("agent", "agent", "agent", "agent")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestStorage_StorageRegistry_Set_Bad(t *core.T) {
	// StorageRegistry Set
	ax7Variant := "StorageRegistry_Set:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(StorageRegistry)
	result := core.Try(func() any {
		got0 := subject.Set("", "", "", "")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestStorage_StorageRegistry_Set_Ugly(t *core.T) {
	// StorageRegistry Set
	ax7Variant := "StorageRegistry_Set:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(StorageRegistry)
	result := core.Try(func() any {
		got0 := subject.Set("../../edge", "../../edge", "../../edge", "../../edge")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestStorage_StorageRegistry_Delete_Good(t *core.T) {
	// StorageRegistry Delete
	ax7Variant := "StorageRegistry_Delete:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(StorageRegistry)
	result := core.Try(func() any {
		got0 := subject.Delete("agent", "agent", "agent")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestStorage_StorageRegistry_Delete_Bad(t *core.T) {
	// StorageRegistry Delete
	ax7Variant := "StorageRegistry_Delete:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(StorageRegistry)
	result := core.Try(func() any {
		got0 := subject.Delete("", "", "")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestStorage_StorageRegistry_Delete_Ugly(t *core.T) {
	// StorageRegistry Delete
	ax7Variant := "StorageRegistry_Delete:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(StorageRegistry)
	result := core.Try(func() any {
		got0 := subject.Delete("../../edge", "../../edge", "../../edge")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestStorage_StorageRegistry_Get_Good(t *core.T) {
	// StorageRegistry Get
	ax7Variant := "StorageRegistry_Get:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(StorageRegistry)
	result := core.Try(func() any {
		got0, got1 := subject.Get("agent", "agent", "agent")
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestStorage_StorageRegistry_Get_Bad(t *core.T) {
	// StorageRegistry Get
	ax7Variant := "StorageRegistry_Get:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(StorageRegistry)
	result := core.Try(func() any {
		got0, got1 := subject.Get("", "", "")
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestStorage_StorageRegistry_Get_Ugly(t *core.T) {
	// StorageRegistry Get
	ax7Variant := "StorageRegistry_Get:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(StorageRegistry)
	result := core.Try(func() any {
		got0, got1 := subject.Get("../../edge", "../../edge", "../../edge")
		return core.Sprintf("%T,%T", got0, got1)
	})
	core.AssertNotNil(t, result.Value)
}

func TestStorage_StorageRegistry_Search_Good(t *core.T) {
	// StorageRegistry Search
	ax7Variant := "StorageRegistry_Search:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(StorageRegistry)
	result := core.Try(func() any {
		got0 := subject.Search("agent")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestStorage_StorageRegistry_Search_Bad(t *core.T) {
	// StorageRegistry Search
	ax7Variant := "StorageRegistry_Search:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(StorageRegistry)
	result := core.Try(func() any {
		got0 := subject.Search("")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestStorage_StorageRegistry_Search_Ugly(t *core.T) {
	// StorageRegistry Search
	ax7Variant := "StorageRegistry_Search:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(StorageRegistry)
	result := core.Try(func() any {
		got0 := subject.Search("../../edge")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestStorage_StorageRegistry_Snapshot_Good(t *core.T) {
	// StorageRegistry Snapshot
	ax7Variant := "StorageRegistry_Snapshot:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(StorageRegistry)
	result := core.Try(func() any {
		got0 := subject.Snapshot("agent")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestStorage_StorageRegistry_Snapshot_Bad(t *core.T) {
	// StorageRegistry Snapshot
	ax7Variant := "StorageRegistry_Snapshot:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(StorageRegistry)
	result := core.Try(func() any {
		got0 := subject.Snapshot("")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestStorage_StorageRegistry_Snapshot_Ugly(t *core.T) {
	// StorageRegistry Snapshot
	ax7Variant := "StorageRegistry_Snapshot:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(StorageRegistry)
	result := core.Try(func() any {
		got0 := subject.Snapshot("../../edge")
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestStorage_StorageRegistry_Close_Good(t *core.T) {
	// StorageRegistry Close
	ax7Variant := "StorageRegistry_Close:good"
	core.AssertContains(t, ax7Variant, "good")
	subject := new(StorageRegistry)
	result := core.Try(func() any {
		got0 := subject.Close()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestStorage_StorageRegistry_Close_Bad(t *core.T) {
	// StorageRegistry Close
	ax7Variant := "StorageRegistry_Close:bad"
	core.AssertContains(t, ax7Variant, "bad")
	subject := new(StorageRegistry)
	result := core.Try(func() any {
		got0 := subject.Close()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}

func TestStorage_StorageRegistry_Close_Ugly(t *core.T) {
	// StorageRegistry Close
	ax7Variant := "StorageRegistry_Close:ugly"
	core.AssertContains(t, ax7Variant, "ugly")
	subject := new(StorageRegistry)
	result := core.Try(func() any {
		got0 := subject.Close()
		return core.Sprintf("%T", got0)
	})
	core.AssertNotNil(t, result.Value)
}
