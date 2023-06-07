package tests

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// TODO Make it a flag
const assetPath = "/test/"

type input struct {
	image       string
	convertOpts []string
}

func TestRuntimes(t *testing.T) {
	c2wBin := "c2w"
	tests := []struct {
		name        string
		inputs      []input
		prepare     func(t *testing.T, workdir string)
		finalize    func(t *testing.T, workdir string)
		imageName   string // default: test.wasm
		runtime     string
		runtimeOpts []string
		args        []string
		want        func(t *testing.T, in io.Writer, out io.Reader)
		noParallel  bool
	}{
		// wasmtime tests
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
		{
			name:    "wasmtime-sh",
			runtime: "wasmtime",
			inputs: []input{
				{image: "alpine:3.17"},
				{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
			},
			args: []string{"sh"},
			want: wantPrompt("/ # ", [2]string{"echo -n hello\n", "hello"}),
		},
		{
			name:    "wasmtime-mapdir",
			runtime: "wasmtime",
			inputs: []input{
				// TODO: add x86_64 test
				{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
			},
			prepare: func(t *testing.T, _ string) {
				mapdirTestDir := "/tmp/wasmtime-mapdirtest/"
				assert.NilError(t, os.MkdirAll(mapdirTestDir, 0755))
				assert.NilError(t, os.WriteFile(filepath.Join(mapdirTestDir, "hi"), []byte("teststring"), 0755))
			},
			finalize: func(t *testing.T, _ string) {
				mapdirTestDir := "/tmp/wasmtime-mapdirtest/"
				assert.NilError(t, os.Remove(filepath.Join(mapdirTestDir, "hi")))
				assert.NilError(t, os.Remove(mapdirTestDir))
			},
			runtimeOpts: []string{"--mapdir=/mapped/dir/test::/tmp/wasmtime-mapdirtest"},
			args:        []string{"cat", "/mapped/dir/test/hi"},
			want:        wantString("teststring"),
		},
		{
			name:    "wasmtime-files",
			runtime: "wasmtime",
			inputs: []input{
				{image: "alpine:3.17"},
				{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
			},
			args: []string{"sh"},
			want: wantPrompt("/ # ",
				[2]string{"echo -n hello > /testhello\n", ""},
				[2]string{"cat /testhello\n", "hello"},
			),
		},
		{
			name: "wasmtime-mapdir-io",
			inputs: []input{
				// TODO: add x86_64 test
				{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
			},
			prepare: func(t *testing.T, _ string) {
				mapdirTestDir := "/tmp/wasmtime-mapdirtest-io/"
				assert.NilError(t, os.MkdirAll(mapdirTestDir, 0755))
				assert.NilError(t, os.WriteFile(filepath.Join(mapdirTestDir, "hi"), []byte("teststring"), 0755))
			},
			finalize: func(t *testing.T, _ string) {
				mapdirTestDir := "/tmp/wasmtime-mapdirtest-io/"

				// check data from guest
				data, err := os.ReadFile(filepath.Join(mapdirTestDir, "from-guest", "testhello"))
				assert.NilError(t, err)
				assert.Equal(t, string(data), "hello")

				// cleanup
				assert.NilError(t, os.Remove(filepath.Join(mapdirTestDir, "from-guest", "testhello")))
				assert.NilError(t, os.Remove(filepath.Join(mapdirTestDir, "from-guest")))
				assert.NilError(t, os.Remove(filepath.Join(mapdirTestDir, "hi")))
				assert.NilError(t, os.Remove(mapdirTestDir))
			},
			runtime:     "wasmtime",
			runtimeOpts: []string{"--mapdir=/mapped/dir/test::/tmp/wasmtime-mapdirtest-io"},
			args:        []string{"sh"},
			want: wantPrompt("/ # ",
				[2]string{"cat /mapped/dir/test/hi\n", "teststring"},
				[2]string{"mkdir /mapped/dir/test/from-guest\n", ""},
				[2]string{"echo -n hello > /mapped/dir/test/from-guest/testhello\n", ""},
			),
		},

		// wamr tests
		{
			name:    "wamr-hello",
			runtime: "iwasm",
			inputs: []input{
				{image: "alpine:3.17"},
				{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
			},
			imageName: "test2.wasm",
			prepare: func(t *testing.T, workdir string) {
				assert.NilError(t, exec.Command("wamrc", "-o", filepath.Join(workdir, "test2.wasm"), filepath.Join(workdir, "test.wasm")).Run())
			},
			args: []string{"echo", "-n", "hello"},
			want: wantString("hello"),
		},
		{
			name:    "wamr-sh",
			runtime: "iwasm",
			inputs: []input{
				{image: "alpine:3.17"},
				{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
			},
			imageName: "test2.wasm",
			prepare: func(t *testing.T, workdir string) {
				assert.NilError(t, exec.Command("wamrc", "-o", filepath.Join(workdir, "test2.wasm"), filepath.Join(workdir, "test.wasm")).Run())
			},
			args: []string{"sh"},
			want: wantPrompt("/ # ", [2]string{"echo -n hello\n", "hello"}),
		},
		{
			name:    "wamr-mapdir",
			runtime: "iwasm",
			inputs: []input{
				// TODO: add x86_64 test (with aot by wamrc)
				{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
			},
			prepare: func(t *testing.T, _ string) {
				mapdirTestDir := "/tmp/wamr-mapdirtest/testdir"
				assert.NilError(t, os.MkdirAll(mapdirTestDir, 0755))
				assert.NilError(t, os.WriteFile(filepath.Join(mapdirTestDir, "hi"), []byte("teststring"), 0755))
			},
			finalize: func(t *testing.T, _ string) {
				mapdirTestDir := "/tmp/wamr-mapdirtest/testdir"
				assert.NilError(t, os.Remove(filepath.Join(mapdirTestDir, "hi")))
				assert.NilError(t, os.Remove(mapdirTestDir))
			},
			runtimeOpts: []string{"--dir=/tmp/wamr-mapdirtest/testdir"},
			args:        []string{"cat", "/tmp/wamr-mapdirtest/testdir/hi"},
			want:        wantString("teststring"),
		},
		{
			name:    "wamr-files",
			runtime: "iwasm",
			inputs: []input{
				{image: "alpine:3.17"},
				{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
			},
			imageName: "test2.wasm",
			prepare: func(t *testing.T, workdir string) {
				assert.NilError(t, exec.Command("wamrc", "-o", filepath.Join(workdir, "test2.wasm"), filepath.Join(workdir, "test.wasm")).Run())
			},
			args: []string{"sh"},
			want: wantPrompt("/ # ",
				[2]string{"echo -n hello > /testhello\n", ""},
				[2]string{"cat /testhello\n", "hello"},
			),
		},
		{
			name:    "wamr-mapdir-io",
			runtime: "iwasm",
			inputs: []input{
				// TODO: add x86_64 test (with aot by wamrc)
				{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
			},
			prepare: func(t *testing.T, _ string) {
				mapdirTestDir := "/tmp/wamr-mapdirtest-io/"
				assert.NilError(t, os.MkdirAll(mapdirTestDir, 0755))
				assert.NilError(t, os.WriteFile(filepath.Join(mapdirTestDir, "hi"), []byte("teststring"), 0755))
			},
			finalize: func(t *testing.T, _ string) {
				mapdirTestDir := "/tmp/wamr-mapdirtest-io/"

				// check data from guest
				data, err := os.ReadFile(filepath.Join(mapdirTestDir, "from-guest", "testhello"))
				assert.NilError(t, err)
				assert.Equal(t, string(data), "hello")

				// cleanup
				assert.NilError(t, os.Remove(filepath.Join(mapdirTestDir, "from-guest", "testhello")))
				assert.NilError(t, os.Remove(filepath.Join(mapdirTestDir, "from-guest")))
				assert.NilError(t, os.Remove(filepath.Join(mapdirTestDir, "hi")))
				assert.NilError(t, os.Remove(mapdirTestDir))
			},
			runtimeOpts: []string{"--dir=/tmp/wamr-mapdirtest-io"},
			args:        []string{"sh"},
			want: wantPrompt("/ # ",
				[2]string{"cat /tmp/wamr-mapdirtest-io/hi\n", "teststring"},
				[2]string{"mkdir /tmp/wamr-mapdirtest-io/from-guest\n", ""},
				[2]string{"echo -n hello > /tmp/wamr-mapdirtest-io/from-guest/testhello\n", ""},
			),
		},

		// wasmer tests
		{
			name:    "wasmer-hello",
			runtime: "wasmer",
			inputs: []input{
				{image: "alpine:3.17"},
				{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
			},
			args: []string{"--", "--no-stdin", "echo", "-n", "hello"}, // wasmer requires "--" before flags we pass to the wasm program.
			want: wantString("hello"),
		},
		// NOTE: stdin unsupported on wasmer
		// TODO: support it
		{
			name:    "wasmer-mapdir",
			runtime: "wasmer",
			inputs: []input{
				// TODO: add x86_64 test
				{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
			},
			prepare: func(t *testing.T, _ string) {
				mapdirTestDir := "/tmp/wasmer-mapdirtest/testdir"
				assert.NilError(t, os.MkdirAll(mapdirTestDir, 0755))
				assert.NilError(t, os.WriteFile(filepath.Join(mapdirTestDir, "hi"), []byte("teststring"), 0755))
			},
			finalize: func(t *testing.T, _ string) {
				mapdirTestDir := "/tmp/wasmer-mapdirtest/testdir"
				assert.NilError(t, os.Remove(filepath.Join(mapdirTestDir, "hi")))
				assert.NilError(t, os.Remove(mapdirTestDir))
			},
			runtimeOpts: []string{"--mapdir=/mapped/dir/test::/tmp/wasmer-mapdirtest/testdir"},
			args:        []string{"--", "--no-stdin", "cat", "/mapped/dir/test/hi"},
			want:        wantString("teststring"),
		},

		// wazero tests
		{
			name:    "wazero-hello",
			runtime: "wazero-test",
			inputs: []input{
				{image: "alpine:3.17"},
				{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
			},
			args: []string{"echo", "-n", "hello"},
			want: wantString("hello"),
		},
		{
			name:    "wazero-sh",
			runtime: "wazero-test",
			inputs: []input{
				{image: "alpine:3.17"},
				{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
			},
			args: []string{"sh"},
			want: wantPrompt("/ # ", [2]string{"echo -n hello\n", "hello"}),
		},
		{
			name:    "wazero-mapdir",
			runtime: "wazero-test",
			inputs: []input{
				// TODO: add x86_64 test
				{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
			},
			prepare: func(t *testing.T, _ string) {
				mapdirTestDir := "/tmp/wazero-mapdirtest/testdir"
				assert.NilError(t, os.MkdirAll(mapdirTestDir, 0755))
				assert.NilError(t, os.WriteFile(filepath.Join(mapdirTestDir, "hi"), []byte("teststring"), 0755))
			},
			finalize: func(t *testing.T, _ string) {
				mapdirTestDir := "/tmp/wazero-mapdirtest/testdir"
				assert.NilError(t, os.Remove(filepath.Join(mapdirTestDir, "hi")))
				assert.NilError(t, os.Remove(mapdirTestDir))
			},
			runtimeOpts: []string{"--mapdir=/mapdir::/tmp/wazero-mapdirtest/testdir"}, // NOTE: wazero supports single-level mapped directory
			args:        []string{"cat", "/mapdir/hi"},
			want:        wantString("teststring"),
		},
		{
			name:    "wazero-files",
			runtime: "wazero-test",
			inputs: []input{
				{image: "alpine:3.17"},
				{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
			},
			args: []string{"sh"},
			want: wantPrompt("/ # ",
				[2]string{"echo -n hello > /testhello\n", ""},
				[2]string{"cat /testhello\n", "hello"},
			),
		},
		{
			name:    "wazero-mapdir-io",
			runtime: "wazero-test",
			inputs: []input{
				// TODO: add x86_64 test
				{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
			},
			prepare: func(t *testing.T, _ string) {
				mapdirTestDir := "/tmp/wazero-mapdirtest-io/"
				assert.NilError(t, os.MkdirAll(mapdirTestDir, 0755))
				assert.NilError(t, os.WriteFile(filepath.Join(mapdirTestDir, "hi"), []byte("teststring"), 0755))
			},
			finalize: func(t *testing.T, _ string) {
				mapdirTestDir := "/tmp/wazero-mapdirtest-io/"

				// check data from guest
				data, err := os.ReadFile(filepath.Join(mapdirTestDir, "from-guest", "testhello"))
				assert.NilError(t, err)
				assert.Equal(t, string(data), "hello")

				// cleanup
				assert.NilError(t, os.Remove(filepath.Join(mapdirTestDir, "from-guest", "testhello")))
				assert.NilError(t, os.Remove(filepath.Join(mapdirTestDir, "from-guest")))
				assert.NilError(t, os.Remove(filepath.Join(mapdirTestDir, "hi")))
				assert.NilError(t, os.Remove(mapdirTestDir))
			},
			runtimeOpts: []string{"--mapdir=/mapdir::/tmp/wazero-mapdirtest-io"}, // NOTE: wazero supports single-level mapped directory
			args:        []string{"sh"},
			want: wantPrompt("/ # ",
				[2]string{"cat /mapdir/hi\n", "teststring"},
				[2]string{"mkdir /mapdir/from-guest\n", ""},
				[2]string{"echo -n hello > /mapdir/from-guest/testhello\n", ""},
			),
		},

		// wasmedge tests
		{
			name:    "wasmedge-hello",
			runtime: "wasmedge",
			inputs: []input{
				{image: "alpine:3.17"},
				{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
			},
			imageName: "test2.wasm",
			prepare: func(t *testing.T, workdir string) {
				assert.NilError(t, exec.Command("wasmedgec", filepath.Join(workdir, "test.wasm"), filepath.Join(workdir, "test2.wasm")).Run())
			},
			args: []string{"--no-stdin", "echo", "-n", "hello"}, // NOTE: stdin unsupported on wasmedge as of now
			want: wantString("hello"),
		},
		// NOTE: stdin unsupported on wasmedge
		// TODO: support it
		{
			name:    "wasmedge-mapdir",
			runtime: "wasmedge",
			inputs: []input{
				// TODO: add x86_64 test
				{image: "riscv64/alpine:20221110", convertOpts: []string{"--target-arch=riscv64"}},
			},
			imageName: "test2.wasm",
			prepare: func(t *testing.T, workdir string) {
				assert.NilError(t, exec.Command("wasmedgec", filepath.Join(workdir, "test.wasm"), filepath.Join(workdir, "test2.wasm")).Run())

				mapdirTestDir := "/tmp/wasmedge-mapdirtest/testdir"
				assert.NilError(t, os.MkdirAll(mapdirTestDir, 0755))
				assert.NilError(t, os.WriteFile(filepath.Join(mapdirTestDir, "hi"), []byte("teststring"), 0755))
			},
			finalize: func(t *testing.T, _ string) {
				mapdirTestDir := "/tmp/wasmedge-mapdirtest/testdir"
				assert.NilError(t, os.Remove(filepath.Join(mapdirTestDir, "hi")))
				assert.NilError(t, os.Remove(mapdirTestDir))
			},
			runtimeOpts: []string{"--dir=/map/dir:/tmp/wasmedge-mapdirtest/testdir"},
			args:        []string{"--no-stdin", "cat", "/map/dir/hi"},
			want:        wantString("teststring"),
		},

		// Other architectures
		{
			name:       "wasmtime-hello-arch-aarch64",
			runtime:    "wasmtime",
			inputs:     []input{{image: "alpine:3.17", convertOpts: []string{"--target-arch=aarch64"}}},
			args:       []string{"echo", "-n", "hello"},
			want:       wantString("hello"),
			noParallel: true, // avoid conflicting image name
		},
	}
	for _, tt := range tests {
		tt := tt
		for _, in := range tt.inputs {
			in := in
			t.Run(strings.Join(append([]string{tt.name, in.image}, in.convertOpts...), ","), func(t *testing.T) {
				if !tt.noParallel {
					t.Parallel()
				}

				tmpdir, err := os.MkdirTemp("", "testc2w")
				assert.NilError(t, err)
				t.Logf("test root: %v", tmpdir)

				testWasm := filepath.Join(tmpdir, "test.wasm")
				c2wCmd := exec.Command(c2wBin, append(in.convertOpts, "--assets="+assetPath, in.image, testWasm)...)
				c2wCmd.Stdout = os.Stdout
				c2wCmd.Stderr = os.Stderr
				assert.NilError(t, c2wCmd.Run())

				if tt.prepare != nil {
					tt.prepare(t, tmpdir)
				}
				if tt.finalize != nil {
					defer tt.finalize(t, tmpdir)
				}

				targetWasm := testWasm
				if tt.imageName != "" {
					targetWasm = filepath.Join(tmpdir, tt.imageName)
				}
				testCmd := exec.Command(tt.runtime, append(append(tt.runtimeOpts, targetWasm), tt.args...)...)
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

				// cleanup cache
				assert.NilError(t, exec.Command("docker", "buildx", "prune", "-f", "--keep-storage=11GB").Run())
			})
		}
	}
}

func wantString(wantstr string) func(t *testing.T, in io.Writer, out io.Reader) {
	return func(t *testing.T, in io.Writer, out io.Reader) {
		outstr, err := io.ReadAll(out)
		assert.NilError(t, err)
		assert.Equal(t, string(outstr), wantstr)
	}
}

func wantPrompt(prompt string, inputoutput ...[2]string) func(t *testing.T, in io.Writer, out io.Reader) {
	return func(t *testing.T, in io.Writer, out io.Reader) {
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
