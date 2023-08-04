package integration

import (
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ktock/container2wasm/tests/integration/utils"
)

func TestWasmtime(t *testing.T) {
	utils.RunTestRuntimes(t, []utils.TestSpec{
		{
			Name:    "wasmtime-hello",
			Runtime: "wasmtime",
			Inputs: []utils.Input{
				{Image: "alpine:3.17", Architecture: utils.X86_64},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}, Architecture: utils.RISCV64},
			},
			Args: utils.StringFlags("echo", "-n", "hello"),
			Want: utils.WantString("hello"),
		},
		{
			Name:    "wasmtime-sh",
			Runtime: "wasmtime",
			Inputs: []utils.Input{
				{Image: "alpine:3.17", Architecture: utils.X86_64},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}, Architecture: utils.RISCV64},
			},
			Args: utils.StringFlags("sh"),
			Want: utils.WantPrompt("/ # ", [2]string{"echo -n hello\n", "hello"}),
		},
		{
			Name:    "wasmtime-mapdir",
			Runtime: "wasmtime",
			Inputs: []utils.Input{
				{Image: "alpine:3.17", Architecture: utils.X86_64},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}, Architecture: utils.RISCV64},
			},
			Prepare: func(t *testing.T, workdir string) {
				mapdirTestDir := filepath.Join(workdir, "wasmtime-mapdirtest")
				assert.NilError(t, os.MkdirAll(mapdirTestDir, 0755))
				assert.NilError(t, os.WriteFile(filepath.Join(mapdirTestDir, "hi"), []byte("teststring"), 0755))
			},
			Finalize: func(t *testing.T, workdir string) {
				mapdirTestDir := filepath.Join(workdir, "wasmtime-mapdirtest")
				assert.NilError(t, os.Remove(filepath.Join(mapdirTestDir, "hi")))
				assert.NilError(t, os.Remove(mapdirTestDir))
			},
			RuntimeOpts: func(t *testing.T, workdir string) []string {
				return []string{"--mapdir=/mapped/dir/test::" + filepath.Join(workdir, "wasmtime-mapdirtest")}
			},
			Args: utils.StringFlags("cat", "/mapped/dir/test/hi"),
			Want: utils.WantString("teststring"),
		},
		{
			Name:    "wasmtime-files",
			Runtime: "wasmtime",
			Inputs: []utils.Input{
				{Image: "alpine:3.17", Architecture: utils.X86_64},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}, Architecture: utils.RISCV64},
			},
			Args: utils.StringFlags("sh"),
			Want: utils.WantPrompt("/ # ",
				[2]string{"echo -n hello > /testhello\n", ""},
				[2]string{"cat /testhello\n", "hello"},
			),
		},
		{
			Name: "wasmtime-mapdir-io",
			Inputs: []utils.Input{
				{Image: "alpine:3.17", Architecture: utils.X86_64},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}, Architecture: utils.RISCV64},
			},
			Prepare: func(t *testing.T, workdir string) {
				mapdirTestDir := filepath.Join(workdir, "wasmtime-mapdirtest-io")
				assert.NilError(t, os.MkdirAll(mapdirTestDir, 0755))
				assert.NilError(t, os.WriteFile(filepath.Join(mapdirTestDir, "hi"), []byte("teststring"), 0755))
			},
			Finalize: func(t *testing.T, workdir string) {
				mapdirTestDir := filepath.Join(workdir, "wasmtime-mapdirtest-io")

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
			Runtime: "wasmtime",
			RuntimeOpts: func(t *testing.T, workdir string) []string {
				return []string{"--mapdir=/mapped/dir/test::" + filepath.Join(workdir, "wasmtime-mapdirtest-io")}
			},
			Args: utils.StringFlags("sh"),
			Want: utils.WantPrompt("/ # ",
				[2]string{"cat /mapped/dir/test/hi\n", "teststring"},
				[2]string{"mkdir /mapped/dir/test/from-guest\n", ""},
				[2]string{"echo -n hello > /mapped/dir/test/from-guest/testhello\n", ""},
			),
		},
		// Other architectures
		{
			Name:       "wasmtime-hello-arch-aarch64",
			Runtime:    "wasmtime",
			Inputs:     []utils.Input{{Image: "alpine:3.17", ConvertOpts: []string{"--target-arch=aarch64"}, Architecture: utils.AArch64}},
			Args:       utils.StringFlags("echo", "-n", "hello"),
			Want:       utils.WantString("hello"),
			NoParallel: true, // avoid conflicting image name
		},
	}...)
}
