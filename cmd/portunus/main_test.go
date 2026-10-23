package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCLIHelper(t *testing.T) {
	if os.Getenv("PORTUNUS_TEST_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"portunus"}, os.Args[i+1:]...)
			main()
			os.Exit(0)
		}
	}
	os.Exit(2)
}

func TestCLISignals(t *testing.T) {
	for _, signal := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			command := exec.Command(executable, "-test.run=^TestCLIHelper$", "--", "-gateway-listen=127.0.0.1:0", "-fake-listen=127.0.0.1:0", "-backup-listen=127.0.0.1:0", "-fake-chunk-delay=1h")
			command.Env = append(os.Environ(), "PORTUNUS_TEST_PROCESS=1")
			output, err := command.StderrPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			defer command.Process.Kill()
			lines := make(chan string, 1)
			go func() {
				reader := bufio.NewReader(output)
				line, _ := reader.ReadString('\n')
				lines <- line
				io.Copy(io.Discard, reader)
			}()
			var ready string
			select {
			case ready = <-lines:
			case <-time.After(5 * time.Second):
				t.Fatal("CLI did not become ready")
			}
			var gatewayURL, fakeURL, backupURL string
			if _, err := fmt.Sscanf(ready, "ready gateway=%s fake=%s backup=%s", &gatewayURL, &fakeURL, &backupURL); err != nil {
				t.Fatalf("invalid ready output %q: %v", ready, err)
			}
			client := &http.Client{Timeout: 5 * time.Second}
			for _, endpoint := range []string{gatewayURL, fakeURL, backupURL} {
				response, err := client.Get(endpoint + "/healthz")
				if err != nil {
					t.Fatal(err)
				}
				io.Copy(io.Discard, response.Body)
				response.Body.Close()
				if response.StatusCode != 200 {
					t.Fatalf("CLI health failed: %s", response.Status)
				}
			}
			request, _ := http.NewRequest("POST", gatewayURL+"/v1/chat", strings.NewReader(`{"messages":[{"role":"user","content":"hello"}],"stream":true}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer demo-key")
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			reader := bufio.NewReader(response.Body)
			line, err := reader.ReadString('\n')
			if err != nil || line != "event: delta\n" {
				t.Fatalf("CLI first event %q, %v", line, err)
			}
			if err := command.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			completed := make(chan error, 1)
			go func() { completed <- command.Wait() }()
			select {
			case err := <-completed:
				if err != nil {
					t.Fatalf("CLI did not stop cleanly: %v", err)
				}
			case <-time.After(8 * time.Second):
				t.Fatal("CLI ignored shutdown signal")
			}
			for _, endpoint := range []string{gatewayURL, fakeURL, backupURL} {
				if res, err := client.Get(endpoint + "/healthz"); err == nil {
					res.Body.Close()
					t.Errorf("listener survived shutdown: %s", endpoint)
				}
			}
			rest, _ := io.ReadAll(reader)
			if strings.Contains(string(rest), "event: done") {
				t.Fatal("shutdown synthesized stream completion")
			}
		})
	}
}

func TestCLIConfigErrors(t *testing.T) {
	for _, args := range [][]string{
		{"-timeout=0"}, {"-fake-status=401"}, {"-fake-cut-after=-2"},
		{"-fake-chunk-delay=-1s"}, {"-fake-first-chunk-delay=-1s"},
		{"-gateway-listen=0.0.0.0:8080"}, {"-fake-listen=localhost:8081"},
		{"-gateway-listen=127.0.0.1:65536"}, {"-gateway-listen=bad"},
		{"-unknown"}, {"unexpected"},
	} {
		var output bytes.Buffer
		if err := run(context.Background(), args, &output); err == nil {
			t.Errorf("accepted CLI arguments %v", args)
		}
		if strings.Contains(output.String(), "ready") {
			t.Errorf("invalid config served traffic: %v", args)
		}
	}
	var output bytes.Buffer
	if err := run(context.Background(), []string{"-help"}, &output); err != nil || !strings.Contains(output.String(), "fake-cut-after") {
		t.Fatalf("help output %q, %v", output.String(), err)
	}
}

func TestCLIOccupiedPorts(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	for _, flag := range []string{"-gateway-listen=", "-fake-listen="} {
		var output bytes.Buffer
		args := []string{"-gateway-listen=127.0.0.1:0", "-fake-listen=127.0.0.1:0", "-backup-listen=127.0.0.1:0", flag + listener.Addr().String()}
		if err := run(context.Background(), args, &output); err == nil || !strings.Contains(err.Error(), "listen") {
			t.Fatalf("occupied port error = %v", err)
		}
		if output.Len() != 0 {
			t.Fatalf("ready announced with occupied port: %q", output.String())
		}
	}
}
