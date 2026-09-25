package annotation

import "testing"

func TestTemporarySessionUsesVolatileMonotonicIDs(t *testing.T) {
	durable := &memoryRepository{records: make(map[uint64]Record)}
	service, err := NewService(durable, &staticReader{content: "one\ntwo\nthree"})
	if err != nil {
		t.Fatal(err)
	}
	service.SetTemporary("session_temp")
	first, err := service.Create(t.Context(), "session_temp", "/repo", testAnchor(), "First")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(t.Context(), "session_temp", first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(t.Context(), "session_temp", "/repo", testAnchor(), "Second")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != 1 || second.ID != 2 || len(durable.records) != 0 {
		t.Fatalf("temporary ids = %d, %d; durable = %+v", first.ID, second.ID, durable.records)
	}
	service.ForgetSession("session_temp")
	service.SetTemporary("session_temp")
	third, err := service.Create(t.Context(), "session_temp", "/repo", testAnchor(), "Third")
	if err != nil || third.ID != 1 {
		t.Fatalf("recreated process-local session = %+v, %v", third, err)
	}
}
