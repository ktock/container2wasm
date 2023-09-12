package main

import (
	"context"
	crand "crypto/rand"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	gvntypes "github.com/containers/gvisor-tap-vsock/pkg/types"
	gvnvirtualnetwork "github.com/containers/gvisor-tap-vsock/pkg/virtualnetwork"
	"github.com/tetratelabs/wazero/experimental/sock"
)

const (
	gatewayIP = "192.168.127.1"
	vmIP      = "192.168.127.3"
	vmMAC     = "02:00:00:00:00:01"
)

func main() {
	var (
		mapDir    = flag.String("mapdir", "", "directory mapping to the image")
		debug     = flag.Bool("debug", false, "debug log")
		mac       = flag.String("mac", vmMAC, "mac address assigned to the container")
		wasiAddr  = flag.String("wasi-addr", "127.0.0.1:1234", "IP address used to communicate between wasi and network stack") // TODO: automatically use empty random port or unix socket
		enableNet = flag.Bool("net", false, "enable network")
	)
	var portFlags sliceFlags
	flag.Var(&portFlags, "p", "map port between host and guest (host:guest). -mac must be set correctly.")
	var envs sliceFlags
	flag.Var(&envs, "env", "environment variables")

	flag.Parse()
	args := flag.Args()
	fsConfig := wazero.NewFSConfig()
	if mapDir != nil && *mapDir != "" {
		m := strings.SplitN(*mapDir, "::", 2)
		if len(m) != 2 {
			panic("specify mapdir as dst::src")
		}
		fsConfig = fsConfig.WithDirMount(m[1], m[0])
	}

	ctx := context.Background()
	if *enableNet {
		forwards := make(map[string]string)
		for _, p := range portFlags {
			parts := strings.Split(p, ":")
			switch len(parts) {
			case 3:
				// IP:PORT1:PORT2
				forwards[strings.Join(parts[0:2], ":")] = strings.Join([]string{vmIP, parts[2]}, ":")
			case 2:
				// PORT1:PORT2
				forwards["0.0.0.0:"+parts[0]] = vmIP + ":" + parts[1]
			}
		}
		config := &gvntypes.Configuration{
			Debug:             *debug,
			MTU:               1500,
			Subnet:            "192.168.127.0/24",
			GatewayIP:         gatewayIP,
			GatewayMacAddress: "5a:94:ef:e4:0c:dd",
			DHCPStaticLeases: map[string]string{
				vmIP: *mac,
			},
			Forwards: forwards,
			NAT: map[string]string{
				"192.168.127.254": "127.0.0.1",
			},
			GatewayVirtualIPs: []string{"192.168.127.254"},
			Protocol:          gvntypes.QemuProtocol,
		}
		vn, err := gvnvirtualnetwork.New(config)
		if err != nil {
			panic(err)
		}
		go func() {
			var conn net.Conn
			for i := 0; i < 100; i++ {
				time.Sleep(1 * time.Second)
				fmt.Fprintf(os.Stderr, "connecting to NW...\n")
				conn, err = net.Dial("tcp", *wasiAddr)
				if err == nil {
					break
				}
				fmt.Fprintf(os.Stderr, "failed connecting to NW: %v; retrying...\n", err)
			}
			if conn == nil {
				panic("failed to connect to vm")
			}
			// We register our VM network as a qemu "-netdev socket".
			if err := vn.AcceptQemu(context.TODO(), conn); err != nil {
				fmt.Fprintf(os.Stderr, "failed AcceptQemu: %v\n", err)
			}
		}()
		u, err := url.Parse("dummy://" + *wasiAddr)
		if err != nil {
			panic(err)
		}
		p, err := strconv.Atoi(u.Port())
		if err != nil {
			panic(err)
		}
		sockCfg := sock.NewConfig().WithTCPListener(u.Hostname(), p)
		ctx = sock.WithConfig(ctx, sockCfg)
	}
	c, err := os.ReadFile(args[0])
	if err != nil {
		panic(err)
	}
	r := wazero.NewRuntime(ctx)
	defer func() {
		r.Close(ctx)
	}()
	wasi_snapshot_preview1.MustInstantiate(ctx, r)
	compiled, err := r.CompileModule(ctx, c)
	if err != nil {
		panic(err)
	}
	conf := wazero.NewModuleConfig().WithSysWalltime().WithSysNanotime().WithSysNanosleep().WithRandSource(crand.Reader).WithStdout(os.Stdout).WithStderr(os.Stderr).WithStdin(os.Stdin).WithFSConfig(fsConfig).WithArgs(append([]string{"arg0"}, args[1:]...)...)
	for _, v := range envs {
		es := strings.SplitN(v, "=", 2)
		if len(es) == 2 {
			conf = conf.WithEnv(es[0], es[1])
		} else {
			panic("env must be a key value pair")
		}
	}
	_, err = r.InstantiateModule(ctx, compiled, conf)
	if err != nil {
		panic(err)
	}
}

type sliceFlags []string

func (f *sliceFlags) String() string {
	var s []string = *f
	return fmt.Sprintf("%v", s)
}

func (f *sliceFlags) Set(value string) error {
	*f = append(*f, value)
	return nil
}
