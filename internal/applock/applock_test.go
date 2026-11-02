//go:build windows

package applock

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// TestCreateNamedReportsExistingInstance exercises the mutex lifecycle with a
// per-run name. The production names are fixed, so using them here would make
// the test fail whenever a real JARVIS instance is running.
func TestCreateNamedReportsExistingInstance(t *testing.T) {
	name := fmt.Sprintf(`Local\JarvisAppRunningTest-%d-%d`, os.Getpid(), time.Now().UnixNano())

	release, already, err := createNamed(name)
	if err != nil {
		t.Fatalf("first createNamed: %v", err)
	}
	if already {
		release()
		t.Fatal("first createNamed reported an existing instance")
	}
	released := false
	defer func() {
		if !released {
			release()
		}
	}()

	if !mutexExists(name) {
		t.Fatal("mutex was not visible after creation")
	}

	releaseSecond, already, err := createNamed(name)
	if err != nil {
		t.Fatalf("second createNamed: %v", err)
	}
	if !already {
		releaseSecond()
		t.Fatal("second createNamed did not report the existing instance")
	}

	// Closing the second handle must not release the first owner's mutex.
	releaseSecond()
	if !mutexExists(name) {
		t.Fatal("first mutex was released by the second handle")
	}

	release()
	released = true
	if mutexExists(name) {
		t.Fatal("mutex still exists after releasing the last handle")
	}
}

// TestCreateUsesAppMutex checks that the public constructor is wired to the
// name queried by IsRunning. It is skipped when a real instance owns the mutex
// because that is exactly the state the constructor reports.
func TestCreateUsesAppMutex(t *testing.T) {
	if IsRunning() {
		t.Skip("a real JARVIS instance owns the app mutex")
	}

	release, already, err := Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer release()
	if already {
		t.Fatal("Create reported an existing instance after IsRunning said otherwise")
	}
	if !IsRunning() {
		t.Fatal("IsRunning did not observe the mutex created by Create")
	}
}
