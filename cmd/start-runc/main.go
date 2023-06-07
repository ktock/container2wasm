package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/containerd/console"
	"github.com/containerd/go-runc"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sys/unix"
)

func main() {
	debug := flag.Bool("debug", false, "add --debug to runc")
	bundle := flag.String("b", "", "specify bundle path")

	flag.Parse()
	args := flag.Args()
	id := args[0]

	if id == "" || *bundle == "" {
		panic("specify id and bundle")
	}
	if err := do(context.TODO(), id, *bundle, *debug); err != nil {
		fmt.Printf("failed to run the container: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func do(ctx context.Context, id, bundle string, debug bool) error {
	r := &runc.Runc{
		Debug: debug,
	}
	s, err := runc.NewTempConsoleSocket()
	if err != nil {
		return err
	}
	cio, err := runc.NewSTDIO()
	if err != nil {
		return err
	}
	_, err = r.Run(ctx, id, bundle, &runc.CreateOpts{
		IO:            cio,
		ConsoleSocket: s,
		Detach:        true,
	})
	if err != nil {
		return err
	}
	c, err := s.ReceiveMaster()
	if err != nil {
		return err
	}
	cur := console.Current()
	if err := cur.SetRaw(); err != nil {
		return err
	}
	termios, err := unix.IoctlGetTermios(int(cur.Fd()), unix.TCGETS)
	if err != nil {
		return err
	}
	// SetRaw clears CS8 and it can break serial console so we restore the configuration here.
	termios.Cflag |= unix.CS8
	if err := unix.IoctlSetTermios(int(cur.Fd()), unix.TCSETS, termios); err != nil {
		return err
	}
	eg, _ := errgroup.WithContext(ctx)
	eg.Go(func() error {
		if _, err := io.Copy(os.Stdout, c); err != nil {
			if debug {
				fmt.Printf("error from stdout io.Copy: %v\n", err)
			}
		}
		// Do not return "panic: read /dev/ptmx: input/output error" on container exit
		return nil
	})
	go func() {
		if _, err := io.Copy(c, os.Stdin); err != nil {
			if debug {
				fmt.Printf("error from stdin io.Copy: %v\n", err)
			}
		}
	}()
	return eg.Wait()
}
