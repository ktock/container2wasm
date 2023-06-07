package tests

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

const assetPath = "/test/"

type input struct {
	image       string
	convertOpts []string
}

func BenchmarkHello(t *testing.B) {
	c2wBin := "c2w"
	tests := []struct {
		name         string
		inputs       []input
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
			runtime: "wasmtime",
			inputs: []input{
				{image: "alpine:3.17"},
				{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
			},
			args: []string{"echo", "-n", "hello"},
			want: wantString("hello"),
		},
		// {
		// 	name:    "wamr-hello",
		// 	runtime: "iwasm",
		// 	inputs: []input{
		// 		{image: "alpine:3.17"},
		// 		{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
		// 	},
		// 	imageName: "test2.wasm",
		// 	prepare: func(t *testing.B, workdir string) {
		// 		assert.NilError(t, exec.Command("wamrc", "-o", filepath.Join(workdir, "test2.wasm"), filepath.Join(workdir, "test.wasm")).Run())
		// 	},
		// 	args: []string{"echo", "-n", "hello"},
		// 	want: wantString("hello"),
		// },
		// {
		// 	name:    "wasmer-hello",
		// 	runtime: "wasmer",
		// 	inputs: []input{
		// 		{image: "alpine:3.17"},
		// 		{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
		// 	},
		// 	args: []string{"--", "--no-stdin", "echo", "-n", "hello"}, // wasmer requires "--" before flags we pass to the wasm program.
		// 	want: wantString("hello"),
		// },
		// {
		// 	name:    "wazero-hello",
		// 	runtime: "wazero-test",
		// 	inputs: []input{
		// 		{image: "alpine:3.17"},
		// 		{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
		// 	},
		// 	args: []string{"echo", "-n", "hello"},
		// 	want: wantString("hello"),
		// },
		// {
		// 	name:    "wasmedge-hello",
		// 	runtime: "wasmedge",
		// 	inputs: []input{
		// 		{image: "alpine:3.17"},
		// 		{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
		// 	},
		// 	imageName: "test2.wasm",
		// 	prepare: func(t *testing.B, workdir string) {
		// 		assert.NilError(t, exec.Command("wasmedgec", filepath.Join(workdir, "test.wasm"), filepath.Join(workdir, "test2.wasm")).Run())
		// 	},
		// 	args: []string{"--no-stdin", "echo", "-n", "hello"}, // NOTE: stdin unsupported on wasmedge as of now
		// 	want: wantString("hello"),
		// },

		// other arch
		{
			name:    "wasmtime-hello-arch-aarch64",
			runtime: "wasmtime",
			inputs:  []input{{image: "alpine:3.17", convertOpts: []string{"--target-arch=aarch64"}}},
			args:    []string{"echo", "-n", "hello"},
			want:    wantString("hello"),
		},

		// no wizer
		{
			name:    "wasmtime-hello-without-wizer",
			runtime: "wasmtime",
			inputs: []input{
				{image: "alpine:3.17", convertOpts: []string{"--build-arg=OPTIMIZATION_MODE=native"}},
				{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64", "--build-arg=OPTIMIZATION_MODE=native"}},
			},
			args: []string{"echo", "-n", "hello"},
			want: wantString("hello"),
		},

		// // no vmtouch
		// {
		// 	name:    "wasmtime-hello-without-vmtouch",
		// 	runtime: "wasmtime",
		// 	inputs: []input{
		// 		{image: "alpine:3.17", convertOpts: []string{"--build-arg=NO_VMTOUCH=true"}},
		// 		{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64", "--build-arg=NO_VMTOUCH=true"}},
		// 	},
		// 	args: []string{"echo", "-n", "hello"},
		// 	want: wantString("hello"),
		// },

		// container
		{
			name:   "docker-hello",
			inputs: []input{{image: "alpine:3.17"}},
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
		for _, in := range tt.inputs {
			in := in
			t.Run(strings.Join(append([]string{tt.name, in.image}, in.convertOpts...), ","), func(t *testing.B) {
				tmpdir, err := os.MkdirTemp("", "testc2w")
				assert.NilError(t, err)
				t.Logf("test root: %v", tmpdir)

				testImage := in.image
				if !tt.noConversion {
					testImage = filepath.Join(tmpdir, "test.wasm")
					c2wCmd := exec.Command(c2wBin, append(in.convertOpts, "--assets="+assetPath, in.image, testImage)...)
					// c2wCmd.Stdout = os.Stdout
					// c2wCmd.Stderr = os.Stderr
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

					tt.want(t, inW, outR)
					inW.Close()

					assert.NilError(t, testCmd.Wait())
				}
				t.StopTimer()

				// cleanup cache
				assert.NilError(t, exec.Command("docker", "buildx", "prune", "-f", "--keep-storage=11GB").Run())
			})
		}
	}
}

func wantString(wantstr string) func(t *testing.B, in io.Writer, out io.Reader) {
	return func(t *testing.B, in io.Writer, out io.Reader) {
		outstr, err := io.ReadAll(out)
		assert.NilError(t, err)
		assert.Equal(t, string(outstr), wantstr)
	}
}
