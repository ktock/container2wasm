package integration

import (
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ktock/container2wasm/tests/integration/utils"
)

func TestWazero(t *testing.T) {
	utils.RunTestRuntimes(t, []utils.TestSpec{
		{
			Name:    "wazero-hello",
			Runtime: "wazero-test",
			Inputs: []utils.Input{
				{Image: "alpine:3.17"},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}},
			},
			Args: utils.StringFlags("echo", "-n", "hello"),
			Want: utils.WantString("hello"),
		},
		{
			Name:    "wazero-sh",
			Runtime: "wazero-test",
			Inputs: []utils.Input{
				{Image: "alpine:3.17"},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}},
			},
			Args: utils.StringFlags("sh"),
			Want: utils.WantPrompt("/ # ", [2]string{"echo -n hello\n", "hello"}),
		},
		{
			Name:    "wazero-mapdir",
			Runtime: "wazero-test",
			Inputs: []utils.Input{
				{Image: "alpine:3.17"},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}},
			},
			Prepare: func(t *testing.T, workdir string) {
				mapdirTestDir := filepath.Join(workdir, "wazero-mapdirtest/testdir")
				assert.NilError(t, os.MkdirAll(mapdirTestDir, 0755))
				assert.NilError(t, os.WriteFile(filepath.Join(mapdirTestDir, "hi"), []byte("teststring"), 0755))
			},
			Finalize: func(t *testing.T, workdir string) {
				mapdirTestDir := filepath.Join(workdir, "wazero-mapdirtest/testdir")
				assert.NilError(t, os.Remove(filepath.Join(mapdirTestDir, "hi")))
				assert.NilError(t, os.Remove(mapdirTestDir))
			},
			RuntimeOpts: func(t *testing.T, workdir string) []string {
				// NOTE: wazero supports single-level mapped directory
				return []string{"--mapdir=/mapdir::" + filepath.Join(workdir, "wazero-mapdirtest/testdir")}
			},
			Args: utils.StringFlags("cat", "/mapdir/hi"),
			Want: utils.WantString("teststring"),
		},
		{
			Name:    "wazero-files",
			Runtime: "wazero-test",
			Inputs: []utils.Input{
				{Image: "alpine:3.17"},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}},
			},
			Args: utils.StringFlags("sh"),
			Want: utils.WantPrompt("/ # ",
				[2]string{"echo -n hello > /testhello\n", ""},
				[2]string{"cat /testhello\n", "hello"},
			),
		},
		{
			Name:    "wazero-mapdir-io",
			Runtime: "wazero-test",
			Inputs: []utils.Input{
				{Image: "alpine:3.17"},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}},
			},
			Prepare: func(t *testing.T, workdir string) {
				mapdirTestDir := filepath.Join(workdir, "wazero-mapdirtest-io")
				assert.NilError(t, os.MkdirAll(mapdirTestDir, 0755))
				assert.NilError(t, os.WriteFile(filepath.Join(mapdirTestDir, "hi"), []byte("teststring"), 0755))
			},
			Finalize: func(t *testing.T, workdir string) {
				mapdirTestDir := filepath.Join(workdir, "wazero-mapdirtest-io")

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
			RuntimeOpts: func(t *testing.T, workdir string) []string {
				// NOTE: wazero supports single-level mapped directory
				return []string{"--mapdir=/mapdir::" + filepath.Join(workdir, "wazero-mapdirtest-io")}
			},
			Args: utils.StringFlags("sh"),
			Want: utils.WantPrompt("/ # ",
				[2]string{"cat /mapdir/hi\n", "teststring"},
				[2]string{"mkdir /mapdir/from-guest\n", ""},
				[2]string{"echo -n hello > /mapdir/from-guest/testhello\n", ""},
			),
		},
	}...)
}
