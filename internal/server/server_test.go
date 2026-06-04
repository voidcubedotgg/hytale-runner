package server

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/voidcubedotgg/hytale-runner/internal/config"
)

// fakeJava writes an executable shell script running the given body and returns
// a Config whose JavaBin points at it.
func fakeJava(t *testing.T, body string) config.Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "java")
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default
	cfg.JavaBin = path
	cfg.DataDir = dir
	return cfg
}

func TestRunExitCodes(t *testing.T) {
	for _, code := range []int{0, 2, 8} {
		t.Run(map[int]string{0: "success", 2: "error", 8: "restart"}[code], func(t *testing.T) {
			cfg := fakeJava(t, "exit "+strconv.Itoa(code))
			if got := Run(cfg); got != code {
				t.Errorf("Run = %d, want %d", got, code)
			}
		})
	}
}

func TestRunPassesExtraArgs(t *testing.T) {
	// Fake java records its argv (cwd is cfg.DataDir).
	cfg := fakeJava(t, `echo "$@" > args.txt`)
	cfg.MinMemory = "1G"
	cfg.MaxMemory = "2G"
	cfg.ServerJarPath = "srv.jar"
	cfg.AssetsPath = "a.zip"
	cfg.ExtraJVMArgs = []string{"-XX:+UseG1GC", "-Dfoo=bar"}
	cfg.ExtraServerArgs = []string{"--world", "nether"}

	if got := Run(cfg); got != 0 {
		t.Fatalf("Run = %d, want 0", got)
	}

	out, err := os.ReadFile(filepath.Join(cfg.DataDir, "args.txt"))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(out))
	// JVM extras before -jar; server extras after --assets.
	want := "-Xms1G -Xmx2G -XX:+UseG1GC -Dfoo=bar -jar srv.jar --assets a.zip --world nether"
	if got != want {
		t.Errorf("argv =\n  %q\nwant\n  %q", got, want)
	}
}

func TestRunStartFailure(t *testing.T) {
	cfg := config.Default
	cfg.JavaBin = "/nonexistent/java-binary"
	cfg.DataDir = t.TempDir()
	if got := Run(cfg); got != 1 {
		t.Errorf("Run = %d, want 1 on start failure", got)
	}
}

func TestRunForwardsSignal(t *testing.T) {
	// Child traps SIGTERM and exits 42; Run must forward the signal it receives.
	// `sleep & wait` so the TERM trap fires immediately; kill the sleep on the
	// way out so it doesn't keep the inherited stdout pipe open.
	cfg := fakeJava(t, "trap 'kill \"$child\" 2>/dev/null; exit 42' TERM\nsleep 10 &\nchild=$!\nwait \"$child\"")

	result := make(chan int, 1)
	go func() { result <- Run(cfg) }()

	time.Sleep(300 * time.Millisecond) // let Run start the child and register the handler
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("kill: %v", err)
	}

	select {
	case got := <-result:
		if got != 42 {
			t.Errorf("Run = %d, want 42 (child trapped SIGTERM)", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after SIGTERM")
	}
}
