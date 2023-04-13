package utils

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

// Defined in Dockerfile.test.
// TODO: Make it a flag
const AssetPath = "/test/"
const C2wBin = "c2w"
const C2wNetProxyBin = "/opt/c2w-net-proxy.wasm"

type Architecture int

const (
	X86_64 Architecture = iota
	RISCV64
	AArch64
)

func archToString(t *testing.T, a Architecture) string {
	switch a {
	case X86_64:
		return "x86_64"
	case RISCV64:
		return "riscv64"
	case AArch64:
		return "aarch64"
	default:
		t.Fatalf("unknown architecture %d", a)
		return ""
	}
}

type Input struct {
	Image        string
	ConvertOpts  []string
	Architecture Architecture
	Dockerfile   string
}

type TestSpec struct {
	Name           string
	Inputs         []Input
	Prepare        func(t *testing.T, workdir string)
	Finalize       func(t *testing.T, workdir string)
	ImageName      string // default: test.wasm
	Runtime        string
	RuntimeOpts    func(t *testing.T, workdir string) []string
	Args           func(t *testing.T, workdir string) []string
	Want           func(t *testing.T, workdir string, in io.Writer, out io.Reader)
	NoParallel     bool
	IgnoreExitCode bool
}

func RunTestRuntimes(t *testing.T, tests ...TestSpec) {
	for _, tt := range tests {
		tt := tt
		for _, in := range tt.Inputs {
			in := in
			t.Run(strings.ReplaceAll(strings.Join(append([]string{tt.Name, in.Image, fmt.Sprintf("arch=%s", archToString(t, in.Architecture))}, in.ConvertOpts...), ","), "/", "-"), func(t *testing.T) {
				if !tt.NoParallel {
					t.Parallel()
				}

				tmpdir, err := os.MkdirTemp("", "testc2w")
				assert.NilError(t, err)
				t.Logf("test root: %v", tmpdir)
				defer func() {
					assert.NilError(t, os.RemoveAll(tmpdir))
				}()

				if in.Dockerfile != "" {
					df := filepath.Join(tmpdir, "Dockerfile-integrationtest")
					assert.NilError(t, os.WriteFile(df, []byte(in.Dockerfile), 0755))
					dcmd := exec.Command("docker", "build", "--progress=plain", "-t", in.Image, "-f", df, AssetPath)
					dcmd.Stdout = os.Stdout
					dcmd.Stderr = os.Stderr
					assert.NilError(t, dcmd.Run())
				}

				testWasm := filepath.Join(tmpdir, "test.wasm")
				c2wCmd := exec.Command(C2wBin, append(in.ConvertOpts, "--assets="+AssetPath, in.Image, testWasm)...)
				c2wCmd.Stdout = os.Stdout
				c2wCmd.Stderr = os.Stderr
				assert.NilError(t, c2wCmd.Run())

				if tt.Prepare != nil {
					tt.Prepare(t, tmpdir)
				}
				if tt.Finalize != nil {
					defer tt.Finalize(t, tmpdir)
				}

				targetWasm := testWasm
				if tt.ImageName != "" {
					targetWasm = filepath.Join(tmpdir, tt.ImageName)
				}
				var runtimeOpts []string
				if tt.RuntimeOpts != nil {
					runtimeOpts = tt.RuntimeOpts(t, tmpdir)
				}
				var args []string
				if tt.Args != nil {
					args = tt.Args(t, tmpdir)
				}
				testCmd := exec.Command(tt.Runtime, append(append(runtimeOpts, targetWasm), args...)...)
				outR, err := testCmd.StdoutPipe()
				assert.NilError(t, err)
				defer outR.Close()
				inW, err := testCmd.StdinPipe()
				assert.NilError(t, err)
				defer inW.Close()
				testCmd.Stderr = os.Stderr

				assert.NilError(t, testCmd.Start())

				tt.Want(t, tmpdir, inW, io.TeeReader(outR, os.Stdout))
				inW.Close()

				if !tt.IgnoreExitCode {
					assert.NilError(t, testCmd.Wait())
				} else {
					if err := testCmd.Wait(); err != nil {
						t.Logf("command test error: %v", err)
					}
				}

				// cleanup cache
				assert.NilError(t, exec.Command("docker", "buildx", "prune", "-f", "--keep-storage=10GB").Run())
				assert.NilError(t, exec.Command("docker", "system", "prune").Run())
			})
		}
	}
}

func WantString(wantstr string) func(t *testing.T, workdir string, in io.Writer, out io.Reader) {
	return func(t *testing.T, workdir string, in io.Writer, out io.Reader) {
		outstr, err := io.ReadAll(out)
		assert.NilError(t, err)
		assert.Equal(t, string(outstr), wantstr)
	}
}

func WantPrompt(prompt string, inputoutput ...[2]string) func(t *testing.T, workdir string, in io.Writer, out io.Reader) {
	return func(t *testing.T, workdir string, in io.Writer, out io.Reader) {
		ctx := context.TODO()

		// Wait for prompt
		_, err := readUntilPrompt(ctx, prompt, out)
		assert.NilError(t, err)

		// Disable echo back
		_, err = in.Write([]byte("stty -echo\n"))
		assert.NilError(t, err)
		_, err = readUntilPrompt(ctx, prompt, out)
		assert.NilError(t, err)

		// Test IO
		for _, iop := range inputoutput {
			input, output := iop[0], iop[1]
			_, err := in.Write([]byte(input))
			assert.NilError(t, err)
			outstr, err := readUntilPrompt(ctx, prompt, out)
			assert.NilError(t, err)
			assert.Equal(t, string(outstr), output)
		}

		// exit the container
		_, err = in.Write([]byte("exit\n"))
		assert.NilError(t, err)
		_, err = io.ReadAll(out)
		assert.NilError(t, err)
	}
}

func WantPromptWithWorkdir(prompt string, inputoutputFunc func(workdir string) [][2]string) func(t *testing.T, workdir string, in io.Writer, out io.Reader) {
	return func(t *testing.T, workdir string, in io.Writer, out io.Reader) {
		WantPrompt(prompt, inputoutputFunc(workdir)...)(t, workdir, in, out)
	}
}

func readUntilPrompt(ctx context.Context, prompt string, outR io.Reader) (out []byte, retErr error) {
	var buf [1]byte
	for {
		_, err := outR.Read(buf[:])
		if err != nil {
			return out, err
		}
		out = append(out, buf[0])
		if i := strings.LastIndex(string(out), prompt); i >= 0 {
			out = out[:i]
			return out, nil
		}
	}
}

func StringFlags(opts ...string) func(t *testing.T, workdir string) []string {
	return func(t *testing.T, workdir string) []string { return opts }
}

var usedPorts = make(map[int]struct{})
var usedPortsMu sync.Mutex

func GetPort(t *testing.T) int {
	usedPortsMu.Lock()
	defer usedPortsMu.Unlock()
	for i := 8001; i < 9000; i++ {
		if _, ok := usedPorts[i]; !ok {
			usedPorts[i] = struct{}{}
			return i
		}
	}
	t.Fatalf("ports exhausted")
	return -1
}

func DonePort(i int) {
	usedPortsMu.Lock()
	defer usedPortsMu.Unlock()
	delete(usedPorts, i)
}

func ReadInt(t *testing.T, p string) int {
	d, err := os.ReadFile(p)
	assert.NilError(t, err)
	i, err := strconv.Atoi(string(d))
	assert.NilError(t, err)
	return i
}

func ReadString(t *testing.T, p string) string {
	d, err := os.ReadFile(p)
	assert.NilError(t, err)
	return string(d)
}

func StartHelloServer(t *testing.T) (pid int, port int) {
	port = GetPort(t)
	t.Logf("launching server on %d", port)
	cmd := exec.Command("httphello", fmt.Sprintf("localhost:%d", port))
	assert.NilError(t, cmd.Start())
	go func() {
		if err := cmd.Wait(); err != nil {
			t.Logf("hello server error: %v\n", err)
		}
		DonePort(port)
	}()
	for {
		if cmd.Process != nil {
			if _, err := http.Get(fmt.Sprintf("http://localhost:%d/", port)); err == nil {
				break
			}
		}
		time.Sleep(1 * time.Millisecond)
	}
	return cmd.Process.Pid, port
}
