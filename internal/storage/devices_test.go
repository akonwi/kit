package storage

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/devices"
)

func TestDeviceRepositoryRoundTrip(t *testing.T) {
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	base := time.Date(2026, 1, 2, 3, 4, 5, 6, time.UTC)
	laptop := devices.Record{
		ID: "device_a", Name: "laptop", TokenHash: devices.HashToken("a"),
		CreatedAt: base, LastUsedAt: base, LastAddress: "100.64.0.2", ExpiresAt: base.Add(time.Hour),
	}
	phone := devices.Record{
		ID: "device_b", Name: "phone", TokenHash: devices.HashToken("b"),
		CreatedAt: base, LastUsedAt: base, ExpiresAt: base.Add(-time.Hour),
	}
	for _, record := range []devices.Record{laptop, phone} {
		if err := store.InsertDevice(t.Context(), record); err != nil {
			t.Fatalf("InsertDevice(%s) error = %v", record.ID, err)
		}
	}
	if err := store.InsertDevice(t.Context(), devices.Record{ID: "device_c", Name: "dup", TokenHash: laptop.TokenHash, CreatedAt: base, LastUsedAt: base, ExpiresAt: base}); err == nil {
		t.Fatal("a duplicate token hash was stored")
	}

	used := base.Add(2 * time.Minute)
	if err := store.TouchDevice(t.Context(), laptop.ID, used, "100.64.0.3", used.Add(time.Hour)); err != nil {
		t.Fatalf("TouchDevice() error = %v", err)
	}
	laptop.LastUsedAt, laptop.LastAddress, laptop.ExpiresAt = used, "100.64.0.3", used.Add(time.Hour)
	listed, err := store.ListDevices(t.Context())
	if err != nil {
		t.Fatalf("ListDevices() error = %v", err)
	}
	if want := []devices.Record{laptop, phone}; !reflect.DeepEqual(listed, want) {
		t.Fatalf("ListDevices() = %+v, want %+v", listed, want)
	}

	purged, err := store.PurgeDevices(t.Context(), base)
	if err != nil || !reflect.DeepEqual(purged, []string{phone.ID}) {
		t.Fatalf("PurgeDevices() = %v, %v; want [%s]", purged, err, phone.ID)
	}
	if deleted, err := store.DeleteDevice(t.Context(), laptop.ID); err != nil || !deleted {
		t.Fatalf("DeleteDevice() = %v, %v", deleted, err)
	}
	if deleted, err := store.DeleteDevice(t.Context(), laptop.ID); err != nil || deleted {
		t.Fatalf("second DeleteDevice() = %v, %v", deleted, err)
	}
	if listed, _ := store.ListDevices(t.Context()); len(listed) != 0 {
		t.Fatalf("ListDevices() after deletes = %+v", listed)
	}
}
