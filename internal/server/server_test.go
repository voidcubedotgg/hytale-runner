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

func TestBuildArgs(t *testing.T) {
	cfg := config.Default
	cfg.MinMemory = "1G"
	cfg.MaxMemory = "2G"
	cfg.ServerJarPath = "srv.jar"
	cfg.AssetsPath = "a.zip"
	cfg.ExtraJVMArgs = []string{"-XX:+UseG1GC"}
	cfg.ExtraServerArgs = []string{"--world", "nether"}

	got := strings.Join(buildArgs(cfg), " ")
	want := "-Xms1G -Xmx2G -XX:+UseG1GC -jar srv.jar --assets a.zip --world nether"
	if got != want {
		t.Errorf("buildArgs = %q, want %q", got, want)
	}
}

func TestSupervisorStartWaitAlive(t *testing.T) {
	cfg := fakeJava(t, "exit 3")
	s := NewSupervisor(cfg)
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := s.Wait(); got != 3 {
		t.Errorf("Wait = %d, want 3", got)
	}
	if s.Alive() {
		t.Error("Alive = true after exit, want false")
	}
}

func TestSupervisorDoubleStartWhileRunning(t *testing.T) {
	// Loop short sleeps and exit on TERM so Stop can reap it promptly without
	// orphaning a long-lived child that keeps the inherited stdout pipe open.
	cfg := fakeJava(t, "trap 'exit 0' TERM\nwhile :; do sleep 0.1; done")
	s := NewSupervisor(cfg)
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop(2 * time.Second)
	if !s.Alive() {
		t.Error("Alive = false right after Start, want true")
	}
	if err := s.Start(); err == nil {
		t.Error("second Start while running = nil, want error")
	}
}

func TestSupervisorRestartAfterExit(t *testing.T) {
	// Exit 8 = restart-for-update; the resident runtime restarts the child.
	cfg := fakeJava(t, "exit 8")
	s := NewSupervisor(cfg)
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := s.Wait(); got != 8 {
		t.Fatalf("Wait = %d, want 8", got)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("restart Start: %v", err)
	}
	if got := s.Wait(); got != 8 {
		t.Errorf("restart Wait = %d, want 8", got)
	}
}

func TestSupervisorStopGraceful(t *testing.T) {
	// Child installs a TERM trap (exit 42), signals readiness, then waits. The
	// ready file makes the trap-installed point deterministic, avoiding a race
	// with Stop's SIGTERM.
	cfg := fakeJava(t, "trap 'kill \"$child\" 2>/dev/null; exit 42' TERM\ntouch ready\nsleep 10 &\nchild=$!\nwait \"$child\"")
	s := NewSupervisor(cfg)
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForFile(t, filepath.Join(cfg.DataDir, "ready"))
	if got := s.Stop(5 * time.Second); got != 42 {
		t.Errorf("Stop = %d, want 42 (child trapped SIGTERM)", got)
	}
}

// waitForFile blocks until path exists or the test times out.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func TestSupervisorStopKillsOnTimeout(t *testing.T) {
	// Child ignores SIGTERM; Stop must escalate to SIGKILL after the grace. Loop
	// short sleeps so the SIGKILL doesn't orphan a long sleep holding the pipe.
	cfg := fakeJava(t, "trap '' TERM\nwhile :; do sleep 0.1; done")
	s := NewSupervisor(cfg)
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	got := s.Stop(300 * time.Millisecond)
	// SIGKILL surfaces as a non-zero (-1 exit code from os/exec) — just assert it stopped.
	if s.Alive() {
		t.Errorf("Alive = true after Stop timeout (code %d), want false", got)
	}
}

func TestSupervisorStopBeforeStart(t *testing.T) {
	s := NewSupervisor(config.Default)
	if got := s.Stop(time.Second); got != 1 {
		t.Errorf("Stop before Start = %d, want 1", got)
	}
	if got := s.Wait(); got != 1 {
		t.Errorf("Wait before Start = %d, want 1", got)
	}
}

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
