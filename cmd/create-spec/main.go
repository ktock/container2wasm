package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/containerd/containerd/archive"
	"github.com/containerd/containerd/archive/compression"
	ctdcontainers "github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/images"
	ctdnamespaces "github.com/containerd/containerd/namespaces"
	ctdoci "github.com/containerd/containerd/oci"
	"github.com/containerd/containerd/platforms"
	inittype "github.com/ktock/container2wasm/cmd/init/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	spec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/opencontainers/runc/libcontainer/user"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

const (
	// runtimeRootfsPath is the rootfs path in the VM used by runc
	runtimeRootfsPath = "/run/rootfs"
	// runtimeBundlePath is the OCI filesystem bundle path in the VM used by runc
	runtimeBundlePath = "/run/bundle"
)

func main() {
	var (
		debug             = flag.Bool("debug", false, "enable debug print on boot")
		debugInit         = flag.Bool("debug-init", false, "enable debug print during init")
		imageConfigPath   = flag.String("image-config-path", "/oci/image.json", "path to image config used by init during runtime")
		runtimeConfigPath = flag.String("runtime-config-path", "/oci/spec.json", "path to runtime spec config used by init during runtime")
		imageRootfsPath   = flag.String("rootfs-path", "/oci/rootfs", "path to rootfs used as overlayfs lowerdir of container rootfs")
		noVmtouch         = flag.Bool("no-vmtouch", false, "do not perform vmtouch")
	)
	flag.Parse()
	args := flag.Args()
	imgDir := args[0]
	platform := args[1]
	rootfs := args[2]

	p, err := platforms.Parse(platform)
	if err != nil {
		panic(err)
	}
	cfg, err := unpack(context.TODO(), imgDir, &p, rootfs)
	if err != nil {
		panic(err)
	}
	cfgD, err := io.ReadAll(cfg)
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile("image.json", cfgD, 0600); err != nil {
		panic(err)
	}
	if err := createSpec(bytes.NewReader(cfgD), rootfs, *debug, *debugInit, *imageConfigPath, *runtimeConfigPath, *imageRootfsPath, *noVmtouch); err != nil {
		panic(err)
	}
}

func unpack(ctx context.Context, imgDir string, platform *spec.Platform, rootfs string) (io.Reader, error) {
	fmt.Println("Trying to unpack image as an OCI image")
	if rootfs == "" {
		return nil, fmt.Errorf("specify rootfs")
	}
	idxR, err := os.Open(filepath.Join(imgDir, "index.json"))
	if err != nil {
		fmt.Println("Failed to unpack the image as an OCI image:", err)
		return unpackDocker(ctx, imgDir, platform, rootfs)
	}
	var idx ocispec.Index
	if err := json.NewDecoder(idxR).Decode(&idx); err != nil {
		return nil, err
	}
	var platformMC platforms.MatchComparer
	if platformMC != nil {
		platformMC = platforms.Only(*platform)
	}
	for _, desc := range idx.Manifests {
		switch desc.MediaType {
		case ocispec.MediaTypeImageManifest, images.MediaTypeDockerSchema2Manifest:
			if desc.Platform != nil && platformMC != nil && !platformMC.Match(*desc.Platform) {
				continue
			}
			mfstD, err := os.ReadFile(filepath.Join(imgDir, "/blobs/sha256", desc.Digest.Encoded()))
			if err != nil {
				return nil, err
			}
			var manifest ocispec.Manifest
			if err := json.Unmarshal(mfstD, &manifest); err != nil {
				return nil, err
			}
			configD, err := os.ReadFile(filepath.Join(imgDir, "/blobs/sha256", manifest.Config.Digest.Encoded()))
			if err != nil {
				return nil, err
			}
			var image ocispec.Image
			if err := json.Unmarshal(configD, &image); err != nil {
				return nil, err
			}
			if platformMC != nil && !platformMC.Match(platforms.Normalize(ocispec.Platform{OS: image.OS, Architecture: image.Architecture})) {
				continue
			}
			for _, layerDesc := range manifest.Layers {
				if err := func() error {
					layerR, err := os.Open(filepath.Join(imgDir, "/blobs/sha256", layerDesc.Digest.Encoded()))
					if err != nil {
						return err
					}
					defer layerR.Close()
					r, err := compression.DecompressStream(layerR)
					if err != nil {
						return err
					}
					var opts []archive.ApplyOpt
					if os.Getenv("_NO_SAME_OWNER") == "1" {
						opts = append(opts, archive.WithNoSameOwner())
					}
					if _, err := archive.Apply(ctx, rootfs, r, opts...); err != nil {
						return err
					}
					return nil
				}(); err != nil {
					return nil, err
				}
			}
			return bytes.NewReader(configD), nil
		default:
			// TODO: support nested manifest lists?
			return nil, fmt.Errorf("unsupported mediatype %v", desc.MediaType)
		}
	}
	return nil, fmt.Errorf("target config not found")
}

func unpackDocker(ctx context.Context, imgDir string, platform *spec.Platform, rootfs string) (io.Reader, error) {
	fmt.Println("Trying to unpack image as a docker image")
	if rootfs == "" {
		return nil, fmt.Errorf("specify rootfs")
	}
	mfstsR, err := os.Open(filepath.Join(imgDir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("unrecognized image format")
	}
	var mfsts []struct {
		Config string
		Layers []string
	}
	if err := json.NewDecoder(mfstsR).Decode(&mfsts); err != nil {
		return nil, err
	}
	fmt.Printf("%+v\n", mfsts)
	var platformMC platforms.MatchComparer
	if platformMC != nil {
		platformMC = platforms.Only(*platform)
	}
	for _, mfst := range mfsts {
		configD, err := os.ReadFile(filepath.Join(imgDir, mfst.Config))
		if err != nil {
			return nil, err
		}
		var image ocispec.Image
		if err := json.Unmarshal(configD, &image); err != nil {
			return nil, err
		}
		if platformMC != nil && !platformMC.Match(platforms.Normalize(ocispec.Platform{OS: image.OS, Architecture: image.Architecture})) {
			// TODO: allow unspecified platform?
			continue
		}
		for _, l := range mfst.Layers {
			if err := func() error {
				layerR, err := os.Open(filepath.Join(imgDir, l))
				if err != nil {
					return err
				}
				defer layerR.Close()
				r, err := compression.DecompressStream(layerR)
				if err != nil {
					return err
				}
				var opts []archive.ApplyOpt
				if os.Getenv("_NO_SAME_OWNER") == "1" {
					opts = append(opts, archive.WithNoSameOwner())
				}
				if _, err := archive.Apply(ctx, rootfs, r, opts...); err != nil {
					return err
				}
				return nil
			}(); err != nil {
				return nil, err
			}
		}
		return bytes.NewReader(configD), nil
	}
	return nil, fmt.Errorf("target config not found")
}

func createSpec(r io.Reader, rootfs string, debug bool, debugInit bool, imageConfigPath, runtimeConfigPath, imageRootfsPath string, noVmtouch bool) error {
	if rootfs == "" {
		return fmt.Errorf("rootfs path must be specified")
	}
	var config spec.Image
	if err := json.NewDecoder(r).Decode(&config); err != nil {
		return err
	}
	s, err := generateSpec(config, rootfs)
	if err != nil {
		return err
	}
	bootConfig, err := generateBootConfig(config, debug, debugInit, imageConfigPath, runtimeConfigPath, imageRootfsPath, noVmtouch)
	if err != nil {
		return err
	}
	sd, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.WriteFile("spec.json", sd, 0600); err != nil {
		return err
	}
	bd, err := json.Marshal(bootConfig)
	if err != nil {
		return err
	}
	if err := os.WriteFile("initconfig.json", bd, 0600); err != nil {
		return err
	}
	return nil
}

func newUnixCaps() []string {
	return []string{
		"CAP_CHOWN",
		"CAP_DAC_OVERRIDE",
		"CAP_FSETID",
		"CAP_FOWNER",
		"CAP_MKNOD",
		"CAP_NET_RAW",
		"CAP_SETGID",
		"CAP_SETUID",
		"CAP_SETFCAP",
		"CAP_SETPCAP",
		"CAP_NET_BIND_SERVICE",
		"CAP_SYS_CHROOT",
		"CAP_KILL",
		"CAP_AUDIT_WRITE",
		"CAP_NET_ADMIN",
	}
}

func generateSpec(config spec.Image, rootfs string) (_ *specs.Spec, err error) {
	ic := config.Config
	ctdCtx := ctdnamespaces.WithNamespace(context.TODO(), "default")
	p := "linux/riscv64"
	if config.Architecture == "amd64" {
		p = "linux/amd64"
	}
	s, err := ctdoci.GenerateSpecWithPlatform(ctdCtx, nil, p, &ctdcontainers.Container{},
		ctdoci.WithHostNamespace(specs.NetworkNamespace),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate spec: %w", err)
	}

	s.Process.Capabilities.Bounding = newUnixCaps()
	s.Process.Capabilities.Permitted = newUnixCaps()
	s.Process.Capabilities.Effective = newUnixCaps()

	if username := ic.User; username != "" {
		passwdPath, err := user.GetPasswdPath()
		if err != nil {
			return nil, fmt.Errorf("failed to get passwd file path: %w", err)
		}
		groupPath, err := user.GetGroupPath()
		if err != nil {
			return nil, fmt.Errorf("failed to get group file path: %w", err)
		}
		passwdR, err := os.Open(filepath.Join(rootfs, passwdPath))
		if err != nil {
			return nil, err
		}
		defer passwdR.Close()
		groupR, err := os.Open(filepath.Join(rootfs, groupPath))
		if err != nil {
			return nil, err
		}
		defer groupR.Close()
		execUser, err := user.GetExecUser(username, nil, passwdR, groupR)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve username %q: %w", username, err)
		}
		s.Process.User.UID = uint32(execUser.Uid)
		s.Process.User.GID = uint32(execUser.Gid)
		for _, g := range execUser.Sgids {
			s.Process.User.AdditionalGids = append(s.Process.User.AdditionalGids, uint32(g))
		}
	}
	s.Process.Env = ic.Env
	args := ic.Entrypoint
	if len(ic.Cmd) > 0 {
		args = append(args, ic.Cmd...)
	}
	if len(args) > 0 {
		s.Process.Args = args
	}
	if ic.WorkingDir != "" {
		s.Process.Cwd = ic.WorkingDir
	}
	s.Process.Terminal = true // TODO: make it configurable

	// TODO: support seccomp and apparmor
	s.Linux.Seccomp = nil
	s.Root = &specs.Root{
		Path: runtimeRootfsPath,
	}
	// TODO: ports
	return s, nil
}

func generateBootConfig(config spec.Image, debug, debugInit bool, imageConfigPath, runtimeConfigPath, imageRootfsPath string, noVmtouch bool) (*inittype.BootConfig, error) {
	runcArgs := []string{"-b", runtimeBundlePath, "foo"}
	if debug {
		runcArgs = append([]string{"--debug"}, runcArgs...)
	}
	var cmdPreRun [][]string
	if !noVmtouch {
		cmdPreRun = [][]string{
			[]string{"vmtouch", "-tv", "/sbin/runc", "/sbin/init", "/sbin/start-runc"},
		}
	}
	bootConfig := &inittype.BootConfig{
		Debug:     debug,
		DebugInit: debugInit,
		Cmd: [][]string{
			append([]string{"/sbin/start-runc"}, runcArgs...),
		},
		CmdPreRun: cmdPreRun,
		Container: inittype.ContainerInfo{
			BundlePath:        runtimeBundlePath,
			ImageConfigPath:   imageConfigPath,
			RuntimeConfigPath: runtimeConfigPath,
		},
		Mounts: []inittype.MountInfo{
			{
				FSType: "proc",
				Src:    "proc",
				Dst:    "/proc",
			},
			{
				FSType: "tmpfs",
				Src:    "tmpfs",
				Dst:    "/run",
			},
			{
				FSType: "tmpfs",
				Src:    "tmpfs",
				Dst:    "/sys",
			},
			{
				FSType: "tmpfs",
				Src:    "tmpfs",
				Dst:    "/tmp",
			},
			{
				FSType: "tmpfs",
				Src:    "tmpfs",
				Dst:    "/var",
			},
			{
				FSType: "tmpfs",
				Src:    "tmpfs",
				Dst:    "/mnt",
			},
			{
				FSType: "cgroup2",
				Src:    "none",
				Dst:    "/sys/fs/cgroup",
				Dir: []inittype.DirInfo{
					{
						Path: "/sys/fs/cgroup",
						Mode: 0666, // TODO: better mode
					},
				},
			},
			{
				// FSType: "bind",
				Src:   "/run/etc/hosts",
				Dst:   "/etc/hosts",
				Data:  "bind",
				Flags: syscall.MS_BIND,
				Dir: []inittype.DirInfo{
					{
						Path: "/run/etc",
						Mode: 0666, // TODO: better mode
					},
				},
				File: []inittype.FileInfo{
					{
						Path:     "/run/etc/hosts",
						Mode:     0666,
						Contents: "127.0.0.1	localhost\n",
					},
				},
			},
			{
				// FSType: "bind",
				Src:   "/run/etc/resolv.conf",
				Dst:   "/etc/resolv.conf",
				Data:  "bind",
				Flags: syscall.MS_BIND,
				File: []inittype.FileInfo{
					{
						Path:     "/run/etc/resolv.conf",
						Mode:     0666,
						Contents: "",
					},
				},
			},
			{
				FSType: "tmpfs",
				Src:    "tmpfs",
				Dst:    runtimeBundlePath,
				Dir: []inittype.DirInfo{
					{
						Path: runtimeBundlePath,
						Mode: 0666,
					},
				},
			},
			{
				FSType: "overlay",
				Src:    "overlay",
				Data:   fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", imageRootfsPath, "/run/rootfs-upper", "/run/rootfs-work"),
				Dst:    runtimeRootfsPath,
				Dir: []inittype.DirInfo{
					{
						Path: runtimeRootfsPath,
						Mode: 0666,
					},
					{
						Path: "/run/rootfs-upper",
						Mode: 0666,
					},
					{
						Path: "/run/rootfs-work",
						Mode: 0666,
					},
				},
			},
		},
	}
	if arch := config.Architecture; arch != "riscv64" && arch != "amd64" {
		procfsPos, found := 0, false
		for i, m := range bootConfig.Mounts {
			if m.FSType == "proc" {
				procfsPos, found = i, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("binfmt_misc configuration failed: procfs must be mounted")
		}
		var newMountInfo []inittype.MountInfo
		newMountInfo = append(newMountInfo, bootConfig.Mounts[:procfsPos+1]...)
		newMountInfo = append(newMountInfo, inittype.MountInfo{
			FSType: "binfmt_misc",
			Src:    "binfmt_misc",
			Dst:    "/proc/sys/fs/binfmt_misc",
			Cmd:    []string{"binfmt", "--install", arch},
		})
		newMountInfo = append(newMountInfo, bootConfig.Mounts[procfsPos+1:]...)
		bootConfig.Mounts = newMountInfo
	}
	return bootConfig, nil
}
