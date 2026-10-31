package ipc

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServerAcceptsAuthenticatedCommand(t *testing.T) {
	dir := t.TempDir()
	received := make(chan string, 1)
	server, err := Start(dir, func(command string) {
		received <- command
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	if _, err := os.Stat(filepath.Join(dir, endpointName)); err != nil {
		t.Fatalf("endpoint file was not created: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := SendCommand(ctx, dir, "открой блокнот"); err != nil {
		t.Fatalf("SendCommand: %v", err)
	}

	select {
	case command := <-received:
		if command != "открой блокнот" {
			t.Fatalf("received %q", command)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive the command")
	}
}

func TestServerCloseRemovesEndpoint(t *testing.T) {
	dir := t.TempDir()
	server, err := Start(dir, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, endpointName)
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	server.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("endpoint still exists after Close: %v", err)
	}
}

func TestSendCommandRejectsEmptyCommand(t *testing.T) {
	if err := SendCommand(context.Background(), t.TempDir(), "   "); err == nil {
		t.Fatal("empty command was accepted")
	}
}

func TestServerRejectsTamperedCommand(t *testing.T) {
	dir := t.TempDir()
	received := make(chan string, 1)
	server, err := Start(dir, func(command string) {
		received <- command
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	endpoint, err := readEndpoint(filepath.Join(dir, endpointName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hex.DecodeString(endpoint.Token); err != nil {
		t.Fatal(err)
	}

	// A client with the endpoint but no valid MAC must not be able to inject.
	req := request{
		Version:   1,
		Command:   "shutdown",
		Timestamp: time.Now().Unix(),
		Nonce:     "0123456789abcdef",
		MAC:       "AAAA",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", endpoint.Port), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write(data); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var resp response
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.OK {
		t.Fatal("tampered command was accepted")
	}
	select {
	case command := <-received:
		t.Fatalf("server executed tampered command %q", command)
	case <-time.After(200 * time.Millisecond):
	}
}
