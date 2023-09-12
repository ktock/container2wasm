package integration

import (
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/exec"
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
				{Image: "alpine:3.17", Architecture: utils.X86_64},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}, Architecture: utils.RISCV64},
			},
			Args: utils.StringFlags("echo", "-n", "hello"),
			Want: utils.WantString("hello"),
		},
		{
			Name:    "wazero-sh",
			Runtime: "wazero-test",
			Inputs: []utils.Input{
				{Image: "alpine:3.17", Architecture: utils.X86_64},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}, Architecture: utils.RISCV64},
			},
			Args: utils.StringFlags("sh"),
			Want: utils.WantPrompt("/ # ", [2]string{"echo -n hello\n", "hello"}),
		},
		{
			Name:    "wazero-mapdir",
			Runtime: "wazero-test",
			Inputs: []utils.Input{
				{Image: "alpine:3.17", Architecture: utils.X86_64},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}, Architecture: utils.RISCV64},
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
			Name:    "wazero-mapdir-io",
			Runtime: "wazero-test",
			Inputs: []utils.Input{
				{Image: "alpine:3.17", Architecture: utils.X86_64},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}, Architecture: utils.RISCV64},
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
		{
			Name:    "wazero-env",
			Runtime: "wazero-test",
			Inputs: []utils.Input{
				{Image: "alpine:3.17", Architecture: utils.X86_64},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}, Architecture: utils.RISCV64},
			},
			RuntimeOpts: utils.StringFlags("--env=AAA=hello", "--env=BBB=world"),
			Args:        utils.StringFlags("/bin/sh", "-c", "echo -n $AAA $BBB"),
			Want:        utils.WantString("hello world"),
		},
		{
			Name:    "wazero-net-proxy",
			Runtime: "c2w-net-proxy-test",
			Inputs: []utils.Input{
				{Image: "debian-wget-x86-64", Architecture: utils.X86_64, Dockerfile: `
FROM debian:sid-slim
RUN apt-get update && apt-get install -y wget
`},
				{Image: "debian-wget-rv64", ConvertOpts: []string{"--target-arch=riscv64"}, Architecture: utils.RISCV64, Dockerfile: `
FROM riscv64/debian:sid-slim
RUN apt-get update && apt-get install -y wget
`},
			},
			Prepare: func(t *testing.T, workdir string) {
				assert.NilError(t, os.WriteFile(filepath.Join(workdir, "httphello-vm-port"), []byte(fmt.Sprintf("%d", utils.GetPort(t))), 0755))
				assert.NilError(t, os.WriteFile(filepath.Join(workdir, "httphello-stack-port"), []byte(fmt.Sprintf("%d", utils.GetPort(t))), 0755))
				pid, port := utils.StartHelloServer(t)
				assert.NilError(t, os.WriteFile(filepath.Join(workdir, "httphello-pid"), []byte(fmt.Sprintf("%d", pid)), 0755))
				assert.NilError(t, os.WriteFile(filepath.Join(workdir, "httphello-port"), []byte(fmt.Sprintf("%d", port)), 0755))
			},
			Finalize: func(t *testing.T, workdir string) {
				p, err := os.FindProcess(utils.ReadInt(t, filepath.Join(workdir, "httphello-pid")))
				assert.NilError(t, err)
				assert.NilError(t, p.Kill())
				if _, err := p.Wait(); err != nil {
					t.Logf("hello server error: %v\n", err)
				}
				utils.DonePort(utils.ReadInt(t, filepath.Join(workdir, "httphello-vm-port")))
				utils.DonePort(utils.ReadInt(t, filepath.Join(workdir, "httphello-stack-port")))
			},
			RuntimeOpts: func(t *testing.T, workdir string) []string {
				return []string{
					"--stack=" + utils.C2wNetProxyBin,
					fmt.Sprintf("--stack-port=%d", utils.ReadInt(t, filepath.Join(workdir, "httphello-stack-port"))),
					fmt.Sprintf("--vm-port=%d", utils.ReadInt(t, filepath.Join(workdir, "httphello-vm-port"))),
				}
			},
			Args: func(t *testing.T, workdir string) []string {
				return []string{"--net=qemu=listenfd=4", "sh", "-c", fmt.Sprintf("for I in $(seq 1 50) ; do if wget -O - http://127.0.0.1:%d/ 2>/dev/null ; then break ; fi ; sleep 1 ; done", utils.ReadInt(t, filepath.Join(workdir, "httphello-port")))}
			},
			Want: utils.WantString("hello"),
		},
		{
			Name: "wazero-net",
			Inputs: []utils.Input{
				{Image: "alpine:3.17", Architecture: utils.X86_64},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}, Architecture: utils.RISCV64},
			},
			Prepare: func(t *testing.T, workdir string) {
				assert.NilError(t, os.WriteFile(filepath.Join(workdir, "httphello-wasi-port"), []byte(fmt.Sprintf("%d", utils.GetPort(t))), 0755))
				pid, port := utils.StartHelloServer(t)
				assert.NilError(t, os.WriteFile(filepath.Join(workdir, "httphello-pid"), []byte(fmt.Sprintf("%d", pid)), 0755))
				assert.NilError(t, os.WriteFile(filepath.Join(workdir, "httphello-port"), []byte(fmt.Sprintf("%d", port)), 0755))
			},
			Finalize: func(t *testing.T, workdir string) {
				p, err := os.FindProcess(utils.ReadInt(t, filepath.Join(workdir, "httphello-pid")))
				assert.NilError(t, err)
				assert.NilError(t, p.Kill())
				if _, err := p.Wait(); err != nil {
					t.Logf("hello server error: %v\n", err)
				}
				utils.DonePort(utils.ReadInt(t, filepath.Join(workdir, "httphello-wasi-port")))
			},
			Runtime: "wazero-test",
			RuntimeOpts: func(t *testing.T, workdir string) []string {
				t.Logf("wasi-addr is  %s", fmt.Sprintf("localhost:%d", utils.ReadInt(t, filepath.Join(workdir, "httphello-wasi-port"))))
				return []string{"-net", "--wasi-addr", fmt.Sprintf("localhost:%d", utils.ReadInt(t, filepath.Join(workdir, "httphello-wasi-port")))}
			},
			Args: func(t *testing.T, workdir string) []string {
				t.Logf("RUNNING: %s", fmt.Sprintf("wget -q -O - http://%s:%d/", hostVirtIP, utils.ReadInt(t, filepath.Join(workdir, "httphello-port"))))
				return []string{"--net=qemu", "sh", "-c", fmt.Sprintf("wget -q -O - http://%s:%d/", hostVirtIP, utils.ReadInt(t, filepath.Join(workdir, "httphello-port")))}
			},
			Want: utils.WantString("hello"),
		},
		{
			Name: "wazero-net-port",
			Inputs: []utils.Input{
				{Image: "httphello-alpine-x86-64", Architecture: utils.X86_64, Dockerfile: `
FROM golang:1.21-bullseye AS dev
COPY ./tests/httphello /httphello
WORKDIR /httphello
RUN GOARCH=amd64 go build -ldflags "-s -w -extldflags '-static'" -tags "osusergo netgo static_build" -o /out/httphello main.go

FROM alpine:3.17
COPY --from=dev /out/httphello /
ENTRYPOINT ["/httphello", "0.0.0.0:80"]
`},
				{Image: "httphello-alpine-rv64", ConvertOpts: []string{"--target-arch=riscv64"}, Architecture: utils.RISCV64, Dockerfile: `
FROM golang:1.21-bullseye AS dev
COPY ./tests/httphello /httphello
WORKDIR /httphello
RUN GOARCH=riscv64 go build -ldflags "-s -w -extldflags '-static'" -tags "osusergo netgo static_build" -o /out/httphello main.go

FROM riscv64/alpine:20221110
COPY --from=dev /out/httphello /
ENTRYPOINT ["/httphello", "0.0.0.0:80"]
`},
			},
			Prepare: func(t *testing.T, workdir string) {
				assert.NilError(t, os.WriteFile(filepath.Join(workdir, "httphello-port"), []byte(fmt.Sprintf("%d", utils.GetPort(t))), 0755))
				assert.NilError(t, os.WriteFile(filepath.Join(workdir, "httphello-wasi-port"), []byte(fmt.Sprintf("%d", utils.GetPort(t))), 0755))
			},
			Finalize: func(t *testing.T, workdir string) {
				utils.DonePort(utils.ReadInt(t, filepath.Join(workdir, "httphello-port")))
				utils.DonePort(utils.ReadInt(t, filepath.Join(workdir, "httphello-wasi-port")))
			},
			Runtime: "wazero-test",
			RuntimeOpts: func(t *testing.T, workdir string) []string {
				t.Logf("wasi-addr is  %s", fmt.Sprintf("localhost:%d", utils.ReadInt(t, filepath.Join(workdir, "httphello-wasi-port"))))
				wasiPort := utils.ReadInt(t, filepath.Join(workdir, "httphello-wasi-port"))
				port := utils.ReadInt(t, filepath.Join(workdir, "httphello-port"))
				t.Logf("port is %s", fmt.Sprintf("localhost:%d:80", port))
				return []string{"-net", "--wasi-addr", fmt.Sprintf("localhost:%d", wasiPort), "-p", fmt.Sprintf("localhost:%d:80", port)}
			},
			Args: func(t *testing.T, workdir string) []string {
				return []string{"--net=qemu"}
			},
			Want: func(t *testing.T, workdir string, in io.Writer, out io.Reader) {
				port := utils.ReadInt(t, filepath.Join(workdir, "httphello-port"))
				cmd := exec.Command("wget", "-q", "-O", "-", fmt.Sprintf("localhost:%d", port))
				cmd.Stderr = os.Stderr
				d, err := cmd.Output()
				assert.NilError(t, err)
				assert.Equal(t, string(d), "hello")
				t.Logf("GOT %s", string(d))
			},
			IgnoreExitCode: true,
		},
		{
			Name: "wazero-net-mac",
			Inputs: []utils.Input{
				{Image: "alpine:3.17", Architecture: utils.X86_64},
				{Image: "riscv64/alpine:20221110", ConvertOpts: []string{"--target-arch=riscv64"}, Architecture: utils.RISCV64},
			},
			Prepare: func(t *testing.T, workdir string) {
				assert.NilError(t, os.WriteFile(filepath.Join(workdir, "httphello-wasi-port"), []byte(fmt.Sprintf("%d", utils.GetPort(t))), 0755))
				pid, port := utils.StartHelloServer(t)
				var v [5]int64
				for i := 0; i < 5; i++ {
					n, err := rand.Int(rand.Reader, big.NewInt(256))
					assert.NilError(t, err)
					v[i] = n.Int64()
				}
				assert.NilError(t, os.WriteFile(filepath.Join(workdir, "mac"), []byte(fmt.Sprintf("02:%02x:%02x:%02x:%02x:%02x", v[0], v[1], v[2], v[3], v[4])), 0755))
				assert.NilError(t, os.WriteFile(filepath.Join(workdir, "httphello-pid"), []byte(fmt.Sprintf("%d", pid)), 0755))
				assert.NilError(t, os.WriteFile(filepath.Join(workdir, "httphello-port"), []byte(fmt.Sprintf("%d", port)), 0755))
			},
			Finalize: func(t *testing.T, workdir string) {
				p, err := os.FindProcess(utils.ReadInt(t, filepath.Join(workdir, "httphello-pid")))
				assert.NilError(t, err)
				assert.NilError(t, p.Kill())
				if _, err := p.Wait(); err != nil {
					t.Logf("hello server error: %v\n", err)
				}
				utils.DonePort(utils.ReadInt(t, filepath.Join(workdir, "httphello-wasi-port")))
			},
			Runtime: "wazero-test",
			RuntimeOpts: func(t *testing.T, workdir string) []string {
				t.Logf("wasi-addr is  %s", fmt.Sprintf("localhost:%d", utils.ReadInt(t, filepath.Join(workdir, "httphello-wasi-port"))))
				return []string{"-net", "--wasi-addr", fmt.Sprintf("localhost:%d", utils.ReadInt(t, filepath.Join(workdir, "httphello-wasi-port")))}
			},
			Args: func(t *testing.T, workdir string) []string {
				t.Logf("RUNNING: %s", fmt.Sprintf("wget -q -O - http://%s:%d/", hostVirtIP, utils.ReadInt(t, filepath.Join(workdir, "httphello-port"))))
				t.Logf("MAC: %s", utils.ReadString(t, filepath.Join(workdir, "mac")))
				return []string{"--net=qemu", fmt.Sprintf("--mac=%s", utils.ReadString(t, filepath.Join(workdir, "mac"))), "sh"}
			},
			Want: utils.WantPromptWithWorkdir("/ # ",
				func(workdir string) [][2]string {
					return [][2]string{
						[2]string{fmt.Sprintf("wget -q -O - http://%s:%d/\n", hostVirtIP, utils.ReadInt(t, filepath.Join(workdir, "httphello-port"))), "hello"},
						[2]string{`/bin/sh -c 'ip a show eth0 | grep ether | sed -E "s/ +/ /g" | cut -f 3 -d " " | tr -d "\n"'` + "\n", utils.ReadString(t, filepath.Join(workdir, "mac"))},
					}
				},
			),
		},
	}...)
}
