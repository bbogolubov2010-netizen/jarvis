package intent

import (
	"testing"
	"time"
)

func TestTakeConfirmationConsumesOnce(t *testing.T) {
	store := NewStore(time.Minute)
	store.Set(Restart, "")

	got, ok := store.TakeConfirmation("YES")
	if !ok || got.Kind != Restart {
		t.Fatalf("got %+v, ok=%v", got, ok)
	}
	if _, ok := store.TakeConfirmation("YES"); ok {
		t.Fatal("action must be consumed exactly once")
	}
}

func TestTakeConfirmationIgnoresOtherInput(t *testing.T) {
	store := NewStore(time.Minute)
	store.Set(Shutdown, "")
	if _, ok := store.TakeConfirmation("maybe"); ok {
		t.Fatal("only explicit confirmations are accepted")
	}
	if !store.Has() {
		t.Fatal("non-confirmation input must not consume the action")
	}
}

func TestExpiredActionIsNotConfirmed(t *testing.T) {
	store := NewStore(time.Millisecond)
	store.Set(Screenshot, "")
	time.Sleep(5 * time.Millisecond)

	if _, ok := store.TakeConfirmation("yes"); ok {
		t.Fatal("expired action must not be confirmable")
	}
	if store.Has() {
		t.Fatal("expired action must be dropped")
	}
}

func TestIsConfirmation(t *testing.T) {
	for _, in := range []string{"YES", " yes ", "y", "Да"} {
		if !IsConfirmation(in) {
			t.Fatalf("%q should be a confirmation", in)
		}
	}
	for _, in := range []string{"", "no", "yesterday"} {
		if IsConfirmation(in) {
			t.Fatalf("%q should not be a confirmation", in)
		}
	}
}

func TestIsCancellation(t *testing.T) {
	for _, in := range []string{"NO", " no ", "n", "cancel", "Нет", "Отмена"} {
		if !IsCancellation(in) {
			t.Fatalf("%q should be a cancellation", in)
		}
	}
	for _, in := range []string{"", "yes", "not now", "nonsense"} {
		if IsCancellation(in) {
			t.Fatalf("%q should not be a cancellation", in)
		}
	}
}
