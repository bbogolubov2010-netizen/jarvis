//go:build windows

package applock

import "testing"

func TestCreateReportsExistingInstance(t *testing.T) {
	release, already, err := Create()
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if already {
		release()
		t.Fatal("first Create reported an existing instance")
	}
	defer release()

	releaseSecond, already, err := Create()
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if !already {
		releaseSecond()
		t.Fatal("second Create did not report the existing instance")
	}
	releaseSecond()
	if !IsRunning() {
		t.Fatal("first mutex was released by the second handle")
	}
}
