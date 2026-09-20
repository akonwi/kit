package storage

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/peer"
	"github.com/akonwi/kit/internal/session"
)

func TestPeerQueryAdmissionIsIdempotentAndOrdered(t *testing.T) {
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cwd := t.TempDir()
	for _, id := range []string{testSessionID('a'), testSessionID('b')} {
		if _, err := store.CreateSession(t.Context(), session.NewSession{ID: id, ScratchpadOwnerID: id, CWD: cwd, Persistent: true, ModelProvider: "test", ModelID: "model"}); err != nil {
			t.Fatal(err)
		}
	}
	limits := peer.DefaultLimits()
	now := time.Now().UTC()
	admission := peer.Admission{ID: testPeerID('1'), IdempotencyKey: "turn:call", SenderSessionID: testSessionID('a'), RecipientSessionID: testSessionID('b'), Message: "What did you learn?", Route: []string{testSessionID('a'), testSessionID('b')}, HopCount: 1, Now: now}
	first, created, err := store.AdmitPeerQuery(t.Context(), admission, limits)
	if err != nil || !created {
		t.Fatalf("admit = %+v, %v, %v", first, created, err)
	}
	admission.ID = testPeerID('2')
	replayed, created, err := store.AdmitPeerQuery(t.Context(), admission, limits)
	if err != nil || created || replayed.ID != first.ID {
		t.Fatalf("replay = %+v, %v, %v", replayed, created, err)
	}
	admission.IdempotencyKey = "turn:call-2"
	admission.ID = testPeerID('2')
	admission.Now = now.Add(time.Second)
	if _, created, err = store.AdmitPeerQuery(t.Context(), admission, limits); err != nil || !created {
		t.Fatal(err)
	}
	claimed, err := store.ClaimNextPeerQuery(t.Context(), testSessionID('b'), now.Add(2*time.Second))
	if err != nil || claimed.ID != first.ID || claimed.State != peer.StateProcessing {
		t.Fatalf("claim = %+v, %v", claimed, err)
	}
	bound, err := store.BindPeerRecipientTurn(t.Context(), claimed.ID, claimed.Generation, "turn_peer")
	if err != nil || bound.RecipientTurnID != "turn_peer" {
		t.Fatalf("bind = %+v, %v", bound, err)
	}
	completed, err := store.CompletePeerQuery(t.Context(), peer.Completion{ID: bound.ID, Generation: bound.Generation, State: peer.StateCompleted, Result: "answer", FinishedAt: now.Add(3 * time.Second)}, limits)
	if err != nil || completed.Result != "answer" {
		t.Fatalf("complete = %+v, %v", completed, err)
	}
	next, err := store.ClaimNextPeerQuery(t.Context(), testSessionID('b'), now.Add(4*time.Second))
	if err != nil || next.ID != testPeerID('2') {
		t.Fatalf("next = %+v, %v", next, err)
	}
	recovered, err := store.RecoverPeerQueries(t.Context(), now.Add(5*time.Second), limits)
	if err != nil || len(recovered) != 1 || recovered[0].ID != next.ID || recovered[0].State != peer.StateProcessing {
		t.Fatalf("recovered = %+v, %v", recovered, err)
	}
	reclaimed, err := store.ClaimNextPeerQuery(t.Context(), testSessionID('b'), now.Add(6*time.Second))
	if err != nil || reclaimed.Generation != next.Generation {
		t.Fatalf("reclaim = %+v, %v", reclaimed, err)
	}
}

func TestArchiveSettlesQueuedPeerQueries(t *testing.T) {
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cwd := t.TempDir()
	sender, recipient := testSessionID('c'), testSessionID('d')
	for _, id := range []string{sender, recipient} {
		if _, err := store.CreateSession(t.Context(), session.NewSession{ID: id, ScratchpadOwnerID: id, CWD: cwd, Persistent: true, ModelProvider: "test", ModelID: "model"}); err != nil {
			t.Fatal(err)
		}
	}
	admission := peer.Admission{ID: testPeerID('3'), IdempotencyKey: "key", SenderSessionID: sender, RecipientSessionID: recipient, Message: "question", Route: []string{sender, recipient}, HopCount: 1, Now: time.Now().UTC()}
	if _, _, err := store.AdmitPeerQuery(t.Context(), admission, peer.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if err := store.ArchiveSession(t.Context(), recipient, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	value, err := store.PeerQuery(t.Context(), admission.ID)
	if err != nil || value.State != peer.StateRecipientArchived {
		t.Fatalf("query = %+v, %v", value, err)
	}
	if _, err := store.ClaimNextPeerQuery(t.Context(), recipient, time.Now().UTC()); !errors.Is(err, peer.ErrUnavailable) {
		t.Fatalf("claim error = %v", err)
	}
}

func testSessionID(fill byte) string { return "session_" + string(makeFill(fill, 32)) }
func testPeerID(fill byte) string    { return "peer_" + string(makeFill(fill, 32)) }
func makeFill(fill byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = fill
	}
	return out
}
