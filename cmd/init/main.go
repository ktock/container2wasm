package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jlaffaye/ftp"
	inittype "github.com/ktock/container2wasm/cmd/init/types"
	imagespec "github.com/opencontainers/image-spec/specs-go/v1"
	runtimespec "github.com/opencontainers/runtime-spec/specs-go"
)

const (
	// initConfigPath is path to the config file used by this init process.
	initConfigPath = "/etc/initconfig.json"

	// wasi0: wasi root directory
	rootFSTag = "wasi0"
	// wasi1: pack directory
	packFSTag = "wasi1"
)

func main() {
	if err := doInit(); err != nil {
		panic(err)
	}
}

func doInit() error {
	os.Setenv("PATH", "/bin:/sbin:/usr/bin:/usr/sbin:/usr/local/bin")
	os.Setenv("HOME", "/root")
	os.Setenv("TERM", "vt100")

	hostAddr := ""
	if v := os.Getenv("HOST_ADDR"); v != "" {
		hostAddr = v
	}
	infoSourceRemoteAddr := ""
	if v := os.Getenv("INIT_INFO_SOURCE_REMOTE"); v != "" {
		infoSourceRemoteAddr = v
	}

	cfgD, err := os.ReadFile(initConfigPath)
	if err != nil {
		return fmt.Errorf("cannot read boot config: %w", err)
	}
	var cfg inittype.BootConfig
	if err := json.Unmarshal(cfgD, &cfg); err != nil {
		return fmt.Errorf("cannot parse boot config: %w", err)
	}
	scmd := exec.Command("stty", "-echo")
	scmd.Stdin = os.Stdin
	if err := scmd.Run(); err != nil {
		return fmt.Errorf("failed to disable tty echo: %w", err)
	}
	if cfg.Debug || cfg.DebugInit {
		log.SetOutput(os.Stdout)
	} else {
		log.SetOutput(io.Discard)
	}
	imageD, err := os.ReadFile(cfg.Container.ImageConfigPath)
	if err != nil {
		return err
	}
	var imageConfig imagespec.Image
	if err := json.Unmarshal(imageD, &imageConfig); err != nil {
		return err
	}

	var wg sync.WaitGroup
	for _, m := range cfg.Mounts {
		if m.Async {
			m := m
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := mount(m); err != nil {
					if m.Optional {
						log.Printf("failed optional mount %+v: %v", m, err)
					} else {
						panic(err)
					}
				}
			}()
		} else {
			if err := mount(m); err != nil {
				if m.Optional {
					log.Printf("failed optional mount %+v: %v", m, err)
				} else {
					return err
				}
			}
		}
	}

	wg.Wait()

	initMounts := []string{rootFSTag, packFSTag}
	if infoSourceRemoteAddr != "" {
		initMounts = []string{rootFSTag}
	}

	// WASI-related filesystems
	for _, tag := range initMounts {
		dst := filepath.Join("/mnt", tag)
		if err := os.Mkdir(dst, 0777); err != nil {
			return err
		}
		log.Printf("mounting %q to %q\n", tag, dst)
		if err := syscall.Mount(tag, dst, "9p", 0, "trans=virtio,version=9p2000.L,msize=8192"); err != nil {
			log.Printf("failed mounting %q: %v\n", tag, err)
			break
		}
	}

	specD, err := os.ReadFile(cfg.Container.RuntimeConfigPath)
	if err != nil {
		return err
	}
	var s runtimespec.Spec
	if err := json.Unmarshal(specD, &s); err != nil {
		return err
	}
	for _, cmd := range cfg.CmdPreRun {
		log.Printf("executing(pre-run): %+v\n", cmd)
		c := exec.Command(cmd[0], cmd[1:]...)
		c.Stdout = log.Writer()
		c.Stderr = log.Writer()
		if err := c.Run(); err != nil {
			return fmt.Errorf("failed to pre-run %v: %w", cmd, err)
		}
	}

	if cfg.Debug {
		log.SetOutput(os.Stdout)
	} else {
		log.SetOutput(io.Discard)
	}

	if infoSourceRemoteAddr != "" {
		// Wizer snapshot can be created by the host here
		//////////////////////////////////////////////////////////////////////
		if hostAddr == "" {
			return fmt.Errorf("must specify HOST_ADDR for remote info")
		}
		for _, c := range [][]string{
			[]string{"ifconfig", "eth0", "up"},
			[]string{"ip", "addr", "add", hostAddr, "dev", "eth0"},
		} {
			wcmd := exec.Command(c[0], c[1:]...)
			wcmd.Stdout = log.Writer()
			wcmd.Stderr = log.Writer()
			if err := wcmd.Run(); err != nil {
				return fmt.Errorf("failed to get info file from remote(%v): %w", c, err)
			}
		}
		if _, err := ftpFileSize(infoSourceRemoteAddr, "initdone"); err != nil {
			log.Printf("error from initdone: %v\n", err)
		}
		for {
			if _, err := ftpFileSize(infoSourceRemoteAddr, "resumed"); err == nil {
				break
			}
		}
		//////////////////////////////////////////////////////////////////////
	} else {
		// Wizer snapshot can be created by the host here
		//////////////////////////////////////////////////////////////////////
		fmt.Printf("==========") // special string not printed
		var b [2]byte
		var bPos int
		bTargetPos := 1
		for {
			if _, err := os.Stdin.Read(b[:]); err != nil {
				return err
			}
			log.Printf("HOST: got %q\n", string(b[:]))
			if b[0] == '=' && b[1] == '\n' {
				bPos++
				if bPos == bTargetPos {
					break
				}
				continue
			}
			bPos = 0
		}
		///////////////////////////////////////////////////////////////////////
	}

	var infoD []byte
	if infoSourceRemoteAddr == "" {
		infoD, err = os.ReadFile(filepath.Join("/mnt", packFSTag, "info"))
	} else {
		infoD, err = ftpRead(infoSourceRemoteAddr, "info")
	}
	if err != nil {
		return err
	}

	log.Printf("INFO:\n%s\n", string(infoD))
	s = patchSpec(s, infoD, imageConfig)

	log.Printf("Running: %+v\n", s.Process.Args)
	sd, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(cfg.Container.BundlePath, "config.json"), sd, 0600); err != nil {
		return err
	}

	var lastErr error
	for _, cmd := range cfg.Cmd {
		log.Printf("executing: %+v\n", cmd)
		c := exec.Command(cmd[0], cmd[1:]...)
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		// TODO: signal?
		if err := c.Run(); err != nil {
			lastErr = fmt.Errorf("failed to run %v: %w", cmd, err)
			break
		}
	}

	if err := exec.Command("poweroff", "-f").Run(); err != nil {
		return fmt.Errorf("failed running poweroff")
	}
	return lastErr
}

func mount(m inittype.MountInfo) error {
	log.Printf("mounting %+v\n", m)
	for _, d := range m.Dir {
		if err := os.MkdirAll(d.Path, os.FileMode(d.Mode)); err != nil {
			return fmt.Errorf("failed to create %q: %w", d.Path, err)
		}
	}
	for _, f := range m.File {
		cf, err := os.Create(f.Path)
		if err != nil {
			return fmt.Errorf("failed to create %q: %w", f.Path, err)
		}
		if _, err := cf.Write([]byte(f.Contents)); err != nil {
			return fmt.Errorf("failed to write contents to %q: %w", f.Path, err)
		}
		if err := cf.Close(); err != nil {
			return fmt.Errorf("failed to close %q: %w", f.Path, err)
		}
	}
	if err := syscall.Mount(m.Src, m.Dst, m.FSType, m.Flags, m.Data); err != nil {
		return fmt.Errorf("cannot mount %q %q %q: %w", m.Src, m.Dst, m.FSType, err)
	}
	if len(m.Cmd) > 0 {
		log.Println(m.Cmd)
		c := exec.Command(m.Cmd[0], m.Cmd[1:]...)
		c.Stdout = log.Writer()
		c.Stderr = log.Writer()
		// TODO: signal?
		if err := c.Run(); err != nil {
			return fmt.Errorf("failed to run command %+v: %w", m.Cmd, err)
		}
	}
	return nil
}

var (
	delimLines = regexp.MustCompile(`[^\\]\n`)
	delimArgs  = regexp.MustCompile(`[^\\] `)
)

func patchSpec(s runtimespec.Spec, infoD []byte, imageConfig imagespec.Image) runtimespec.Spec {
	var options []string
	lmchs := delimLines.FindAllIndex(infoD, -1)
	prev := 0
	for _, m := range lmchs {
		s := m[0] + 1
		// newline are quoted so we restore them here
		options = append(options, strings.ReplaceAll(string(infoD[prev:s]), "\\\n", "\n"))
		prev = m[1]
	}
	options = append(options, strings.ReplaceAll(string(infoD[prev:]), "\\\n", "\n"))
	var entrypoint []string
	var args []string
	for _, l := range options {
		elms := strings.SplitN(l, ":", 2)
		if len(elms) != 2 {
			continue
		}
		inst := elms[0]
		o := strings.TrimLeft(elms[1], " ")
		switch inst {
		case "m":
			if o == "" {
				// no path is specified; nop
				continue
			}
			s.Mounts = append(s.Mounts, runtimespec.Mount{
				Type:        "bind",
				Source:      filepath.Join("/mnt/wasi0", o),
				Destination: filepath.Join("/", o), // TODO: ensure not outside of "/"
				Options:     []string{"bind"},
			})
			log.Printf("Prepared mount wasi0 => %q", o)
		case "c":
			args = nil
			mchs := delimArgs.FindAllIndex([]byte(o), -1)
			prev := 0
			for _, m := range mchs {
				s := m[0] + 1
				// spaces are quoted so we restore them here
				args = append(args, strings.ReplaceAll(string(o[prev:s]), "\\ ", " "))
				prev = m[1]
			}
			args = append(args, strings.ReplaceAll(string(o[prev:]), "\\ ", " "))
		case "e":
			entrypoint = []string{o}
		default:
			log.Printf("unsupported prefix: %q", inst)
		}
	}
	if len(entrypoint) == 0 {
		entrypoint = imageConfig.Config.Entrypoint
	}
	if len(args) == 0 {
		args = imageConfig.Config.Cmd
	}
	s.Process.Args = append(entrypoint, args...)
	return s
}

func ftpFileSize(addr, p string) (int64, error) {
	c, err := ftp.Dial(addr, ftp.DialWithTimeout(5*time.Second))
	if err != nil {
		return 0, err
	}
	defer func() {
		if err := c.Quit(); err != nil {
			log.Printf("ftpFileSize: error from Quit: %v\n", err)
		}
	}()
	if err := c.Login("anonymous", "dummypass"); err != nil {
		return 0, err
	}
	return c.FileSize(p)
}

func ftpRead(addr, p string) ([]byte, error) {
	c, err := ftp.Dial(addr, ftp.DialWithTimeout(5*time.Second))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := c.Quit(); err != nil {
			log.Printf("ftpRead: error from Quit: %v\n", err)
		}
	}()
	if err := c.Login("anonymous", "dummypass"); err != nil {
		return nil, err
	}
	r, err := c.Retr(p)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}
