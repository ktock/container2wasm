package utils

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

// Defined in Dockerfile.test.
// TODO: Make it a flag
const assetPath = "/test/"

type Input struct {
	Image       string
	ConvertOpts []string
}

type TestSpec struct {
	Name        string
	Inputs      []Input
	Prepare     func(t *testing.T, workdir string)
	Finalize    func(t *testing.T, workdir string)
	ImageName   string // default: test.wasm
	Runtime     string
	RuntimeOpts func(t *testing.T, workdir string) []string
	Args        func(t *testing.T, workdir string) []string
	Want        func(t *testing.T, workdir string, in io.Writer, out io.Reader)
	NoParallel  bool
}

func RunTestRuntimes(t *testing.T, tests ...TestSpec) {
	c2wBin := "c2w"
	for _, tt := range tests {
		tt := tt
		for _, in := range tt.Inputs {
			in := in
			t.Run(strings.Join(append([]string{tt.Name, in.Image}, in.ConvertOpts...), ","), func(t *testing.T) {
				if !tt.NoParallel {
					t.Parallel()
				}

				tmpdir, err := os.MkdirTemp("", "testc2w")
				assert.NilError(t, err)
				t.Logf("test root: %v", tmpdir)
				defer assert.NilError(t, os.RemoveAll(tmpdir))

				testWasm := filepath.Join(tmpdir, "test.wasm")
				c2wCmd := exec.Command(c2wBin, append(in.ConvertOpts, "--assets="+assetPath, in.Image, testWasm)...)
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

				assert.NilError(t, testCmd.Start())

				tt.Want(t, tmpdir, inW, io.TeeReader(outR, os.Stdout))
				inW.Close()

				assert.NilError(t, testCmd.Wait())

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
