// build produces the portable Windows executable without PowerShell or .NET.
// Run from the repository root: go run ./cmd/build
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/infrastructure/data"
	"github.com/sa1tbaby/go-network-stat/backend/infrastructure/fileutil"
)

func main() {
	output := flag.String("output", "dist/InternetStability.exe", "Windows executable path")
	skipTests := flag.Bool("skip-tests", false, "Skip unit and integration tests")
	flag.Parse()

	root, err := os.Getwd()
	if err != nil {
		fail(err)
	}

	if _, err := os.Stat(filepath.Join(root, "cmd", "client", "main_windows.go")); err != nil {
		fail(fmt.Errorf("run from the repository root: %w", err))
	}

	layout, err := data.New(filepath.Join(root, "data"))
	if err != nil {
		fail(err)
	}

	if err = layout.Create(); err != nil {
		fail(err)
	}

	absolute, err := filepath.Abs(*output)
	if err != nil {
		fail(err)
	}

	if err = os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
		fail(err)
	}

	goBinary := filepath.Join(runtime.GOROOT(), "bin", "go")
	if runtime.GOOS == "windows" {
		goBinary += ".exe"
	}

	if !*skipTests {
		runGo(goBinary, root, false, "test", "./...")
	}

	stage, err := os.MkdirTemp(layout.Build, "client-")
	if err != nil {
		fail(err)
	}

	defer os.RemoveAll(stage)
	prepareBuildStage(root, stage)

	runGo(goBinary, root, false, "run", "./tools/resources/main.go", "-output", filepath.Join(stage, "client_windows_amd64.syso"))
	runGo(goBinary, stage, true, "build", "-trimpath", "-ldflags", "-H=windowsgui -s -w", "-o", absolute, ".")

	writeBuildResult(absolute, layout.Build)
}

func runGo(goBinary, directory string, windows bool, arguments ...string) {
	command := exec.Command(goBinary, arguments...)
	command.Dir = directory
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	command.Env = hostEnvironment()
	if windows {
		command.Env = append(command.Env, "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")
	}

	if err := command.Run(); err != nil {
		fail(err)
	}
}

func prepareBuildStage(root, stage string) {
	source, err := os.ReadFile(filepath.Join(root, "cmd", "client", "main_windows.go"))
	if err != nil {
		fail(err)
	}

	if err = os.WriteFile(filepath.Join(stage, "main_windows.go"), source, 0600); err != nil {
		fail(err)
	}

	// A temporary module lets Go link the manifest without generating .syso files
	// in source directories. Its only dependency is this local checkout.
	module := "module network-statistic-build\n\ngo 1.24.0\n\nrequire github.com/sa1tbaby/go-network-stat v0.0.0\nreplace github.com/sa1tbaby/go-network-stat => " + strconv.Quote(filepath.ToSlash(root)) + "\n"
	if err = os.WriteFile(filepath.Join(stage, "go.mod"), []byte(module), 0600); err != nil {
		fail(err)
	}
}

func writeBuildResult(absolute, buildDirectory string) {
	executable, err := os.ReadFile(absolute)
	if err != nil {
		fail(err)
	}

	digest := sha256.Sum256(executable)
	result := map[string]any{
		"executable":      absolute,
		"bytes":           len(executable),
		"sha256":          hex.EncodeToString(digest[:]),
		"go":              runtime.Version(),
		"builtAt":         time.Now().UTC().Format(time.RFC3339),
		"backend":         "go",
		"externalRuntime": false,
	}
	if err = fileutil.WriteJSON(filepath.Join(buildDirectory, "build-result.json"), result); err != nil {
		fail(err)
	}

	fmt.Printf("Built %s (%d bytes, SHA256 %x)\n", absolute, len(executable), digest)
}

func hostEnvironment() []string {
	var result []string
	for _, value := range os.Environ() {
		key := strings.SplitN(value, "=", 2)[0]
		if key != "GOOS" && key != "GOARCH" && key != "CGO_ENABLED" {
			result = append(result, value)
		}
	}

	return result
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
