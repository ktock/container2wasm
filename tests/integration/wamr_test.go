package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/ktock/container2wasm/tests/integration/utils"
)

func TestWamr(t *testing.T) {
	utils.RunTestRuntimes(t, []utils.TestSpec{
		{
			Name:    "wamr-hello",
			Runtime: "iwasm",
			Inputs: []utils.Input{
				{Image: "alpine:3.17", Architecture: utils.X86_64},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}, Architecture: utils.RISCV64},
			},
			ImageName: "test2.wasm",
			Prepare: func(t *testing.T, env utils.Env) {
				assert.NilError(t, exec.Command("wamrc", "-o", filepath.Join(env.Workdir, "test2.wasm"), filepath.Join(env.Workdir, "test.wasm")).Run())
			},
			Args: utils.StringFlags("echo", "-n", "hello"),
			Want: utils.WantString("hello"),
		},
		{
			Name:    "wamr-sh",
			Runtime: "iwasm",
			Inputs: []utils.Input{
				{Image: "alpine:3.17", Architecture: utils.X86_64},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}, Architecture: utils.RISCV64},
			},
			ImageName: "test2.wasm",
			Prepare: func(t *testing.T, env utils.Env) {
				assert.NilError(t, exec.Command("wamrc", "-o", filepath.Join(env.Workdir, "test2.wasm"), filepath.Join(env.Workdir, "test.wasm")).Run())
			},
			Args: utils.StringFlags("sh"),
			Want: utils.WantPrompt("/ # ", [2]string{"echo -n hello\n", "hello"}),
		},
		{
			Name:    "wamr-mapdir",
			Runtime: "iwasm",
			Inputs: []utils.Input{
				{Image: "alpine:3.17", Architecture: utils.X86_64},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}, Architecture: utils.RISCV64},
			},
			ImageName: "test2.wasm",
			Prepare: func(t *testing.T, env utils.Env) {
				assert.NilError(t, exec.Command("wamrc", "-o", filepath.Join(env.Workdir, "test2.wasm"), filepath.Join(env.Workdir, "test.wasm")).Run())
				mapdirTestDir := filepath.Join(env.Workdir, "wamr-mapdirtest/testdir")
				assert.NilError(t, os.MkdirAll(mapdirTestDir, 0755))
				assert.NilError(t, os.WriteFile(filepath.Join(mapdirTestDir, "hi"), []byte("teststring"), 0755))
			},
			Finalize: func(t *testing.T, env utils.Env) {
				mapdirTestDir := filepath.Join(env.Workdir, "wamr-mapdirtest/testdir")
				assert.NilError(t, os.Remove(filepath.Join(mapdirTestDir, "hi")))
				assert.NilError(t, os.Remove(mapdirTestDir))
			},
			RuntimeOpts: func(t *testing.T, env utils.Env) []string {
				return []string{"--dir=" + filepath.Join(env.Workdir, "wamr-mapdirtest/testdir")}
			},
			Args: func(t *testing.T, env utils.Env) []string {
				return []string{"cat", filepath.Join(env.Workdir, "wamr-mapdirtest/testdir/hi")}
			},
			Want: utils.WantString("teststring"),
		},
		{
			Name:    "wamr-files",
			Runtime: "iwasm",
			Inputs: []utils.Input{
				{Image: "alpine:3.17", Architecture: utils.X86_64},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}, Architecture: utils.RISCV64},
			},
			ImageName: "test2.wasm",
			Prepare: func(t *testing.T, env utils.Env) {
				assert.NilError(t, exec.Command("wamrc", "-o", filepath.Join(env.Workdir, "test2.wasm"), filepath.Join(env.Workdir, "test.wasm")).Run())
			},
			Args: utils.StringFlags("sh"),
			Want: utils.WantPrompt("/ # ",
				[2]string{"echo -n hello > /testhello\n", ""},
				[2]string{"cat /testhello\n", "hello"},
			),
		},
		{
			Name:    "wamr-mapdir-io",
			Runtime: "iwasm",
			Inputs: []utils.Input{
				{Image: "alpine:3.17", Architecture: utils.X86_64},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}, Architecture: utils.RISCV64},
			},
			ImageName: "test2.wasm",
			Prepare: func(t *testing.T, env utils.Env) {
				assert.NilError(t, exec.Command("wamrc", "-o", filepath.Join(env.Workdir, "test2.wasm"), filepath.Join(env.Workdir, "test.wasm")).Run())
				mapdirTestDir := filepath.Join(env.Workdir, "wamr-mapdirtest-io")
				assert.NilError(t, os.MkdirAll(mapdirTestDir, 0755))
				assert.NilError(t, os.WriteFile(filepath.Join(mapdirTestDir, "hi"), []byte("teststring"), 0755))
			},
			Finalize: func(t *testing.T, env utils.Env) {
				mapdirTestDir := filepath.Join(env.Workdir, "wamr-mapdirtest-io")

				// check data from guest
				data, err := os.ReadFile(filepath.Join(mapdirTestDir, "from-guest", "testhello"))
				assert.NilError(t, err)
				assert.Equal(t, string(data), "hello2")

				// cleanup
				assert.NilError(t, os.Remove(filepath.Join(mapdirTestDir, "from-guest", "testhello")))
				assert.NilError(t, os.Remove(filepath.Join(mapdirTestDir, "from-guest")))
				assert.NilError(t, os.Remove(filepath.Join(mapdirTestDir, "hi")))
				assert.NilError(t, os.Remove(mapdirTestDir))
			},
			RuntimeOpts: func(t *testing.T, env utils.Env) []string {
				return []string{"--dir=" + filepath.Join(env.Workdir, "wamr-mapdirtest-io")}
			},
			Args: utils.StringFlags("sh"),
			Want: utils.WantPromptWithWorkdir("/ # ",
				func(workdir string) [][2]string {
					return [][2]string{
						[2]string{"cat " + filepath.Join(workdir, "wamr-mapdirtest-io/hi") + "\n", "teststring"},
						[2]string{"mkdir " + filepath.Join(workdir, "wamr-mapdirtest-io/from-guest") + "\n", ""},
						[2]string{"echo -n hello > " + filepath.Join(workdir, "wamr-mapdirtest-io/from-guest/testhello") + "\n", ""},
						[2]string{"echo -n hello2 > " + filepath.Join(workdir, "wamr-mapdirtest-io/from-guest/testhello") + "\n", ""}, // overwrite
					}
				},
			),
		},
		{
			Name:    "wamr-env",
			Runtime: "iwasm",
			Inputs: []utils.Input{
				{Image: "alpine:3.17", Architecture: utils.X86_64},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}, Architecture: utils.RISCV64},
			},
			ImageName: "test2.wasm",
			Prepare: func(t *testing.T, env utils.Env) {
				assert.NilError(t, exec.Command("wamrc", "-o", filepath.Join(env.Workdir, "test2.wasm"), filepath.Join(env.Workdir, "test.wasm")).Run())
			},
			RuntimeOpts: utils.StringFlags("--env=AAA=hello", "--env=BBB=world"),
			Args:        utils.StringFlags("/bin/sh", "-c", "echo -n $AAA $BBB"),
			Want:        utils.WantString("hello world"),
		},
	}...)
}
