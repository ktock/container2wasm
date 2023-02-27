package tests

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

const assetPath = "/test/"

func BenchmarkHello(t *testing.B) {
	c2wBin := "c2w"
	tests := []struct {
		name         string
		input        string
		convertOpts  []string
		prepare      func(t *testing.B, workdir string)
		finalize     func(t *testing.B, workdir string)
		imageName    string // default: test.wasm
		runtime      string
		runtimeOpts  []string
		args         []string
		want         func(t *testing.B, in io.Writer, out io.Reader)
		noConversion bool
	}{
		{
			name:    "wasmtime-hello",
			input:   "riscv64/alpine:20221110",
			runtime: "wasmtime",
			args:    []string{"echo", "-n", "hello"},
			want:    wantString("hello"),
		},
		{
			name:    "wamr-hello",
			input:   "riscv64/alpine:20221110",
			runtime: "iwasm",
			args:    []string{"echo", "-n", "hello"},
			want:    wantString("hello"),
		},
		{
			name:    "wasmer-hello",
			input:   "riscv64/alpine:20221110",
			runtime: "wasmer",
			args:    []string{"--", "echo", "-n", "hello"}, // wasmer requires "--" before flags we pass to the wasm program.
			want:    wantString("hello"),
		},
		{
			name:    "wazero-hello",
			input:   "riscv64/alpine:20221110",
			runtime: "wazero-test",
			args:    []string{"echo", "-n", "hello"},
			want:    wantString("hello"),
		},
		{
			name:      "wasmedge-hello",
			input:     "riscv64/alpine:20221110",
			imageName: "test2.wasm",
			prepare: func(t *testing.B, workdir string) {
				assert.NilError(t, exec.Command("wasmedgec", filepath.Join(workdir, "test.wasm"), filepath.Join(workdir, "test2.wasm")).Run())
			},
			runtime: "wasmedge",
			args:    []string{"--no-stdin", "echo", "-n", "hello"}, // NOTE: stdin unsupported on wasmedge as of now
			want:    wantString("hello"),
		},

		// non-riscv arch
		{
			name:        "wasmtime-hello-arch-amd64",
			input:       "alpine:3.17",
			runtime:     "wasmtime",
			convertOpts: []string{"--target-arch=amd64"},
			args:        []string{"echo", "-n", "hello"},
			want:        wantString("hello"),
		},
		{
			name:        "wasmtime-hello-arch-aarch64",
			input:       "alpine:3.17",
			runtime:     "wasmtime",
			convertOpts: []string{"--target-arch=aarch64"},
			args:        []string{"echo", "-n", "hello"},
			want:        wantString("hello"),
		},

		// no wizer
		{
			name:        "wasmtime-hello-without-wizer",
			input:       "riscv64/alpine:20221110",
			runtime:     "wasmtime",
			convertOpts: []string{"--build-arg=OPTIMIZATION_MODE=native"},
			args:        []string{"echo", "-n", "hello"},
			want:        wantString("hello"),
		},

		// no vmtouch
		{
			name:        "wasmtime-hello-without-vmtouch",
			input:       "riscv64/alpine:20221110",
			runtime:     "wasmtime",
			convertOpts: []string{"--build-arg=NO_VMTOUCH=true"},
			args:        []string{"echo", "-n", "hello"},
			want:        wantString("hello"),
		},

		// container
		{
			name:  "docker-hello",
			input: "alpine:3.17",
			prepare: func(t *testing.B, workdir string) {
				assert.NilError(t, exec.Command("docker", "pull", "alpine:3.17").Run())
			},
			runtime:      "docker",
			runtimeOpts:  []string{"run", "--rm"},
			args:         []string{"echo", "-n", "hello"},
			want:         wantString("hello"),
			noConversion: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.B) {
			tmpdir, err := os.MkdirTemp("", "testc2w")
			assert.NilError(t, err)
			t.Logf("test root: %v", tmpdir)

			testImage := tt.input
			if !tt.noConversion {
				testImage = filepath.Join(tmpdir, "test.wasm")
				c2wCmd := exec.Command(c2wBin, append(tt.convertOpts, "--assets="+assetPath, tt.input, testImage)...)
				c2wCmd.Stdout = os.Stdout
				c2wCmd.Stderr = os.Stderr
				assert.NilError(t, c2wCmd.Run())
			}

			if tt.prepare != nil {
				tt.prepare(t, tmpdir)
			}
			if tt.finalize != nil {
				defer tt.finalize(t, tmpdir)
			}

			if tt.imageName != "" {
				testImage = filepath.Join(tmpdir, tt.imageName)
			}

			t.ResetTimer()
			for i := 0; i < t.N; i++ {
				testCmd := exec.Command(tt.runtime, append(append(tt.runtimeOpts, testImage), tt.args...)...)
				outR, err := testCmd.StdoutPipe()
				assert.NilError(t, err)
				defer outR.Close()
				inW, err := testCmd.StdinPipe()
				assert.NilError(t, err)
				defer inW.Close()

				assert.NilError(t, testCmd.Start())

				tt.want(t, inW, io.TeeReader(outR, os.Stdout))
				inW.Close()

				assert.NilError(t, testCmd.Wait())
			}
		})
	}
}

func wantString(wantstr string) func(t *testing.B, in io.Writer, out io.Reader) {
	return func(t *testing.B, in io.Writer, out io.Reader) {
		outstr, err := io.ReadAll(out)
		assert.NilError(t, err)
		assert.Equal(t, string(outstr), wantstr)
	}
}
