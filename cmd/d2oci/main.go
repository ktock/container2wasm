package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"time"

	"github.com/containerd/containerd/archive"
	"github.com/ktock/container2wasm/version"
	"github.com/urfave/cli"

	"github.com/containerd/containerd/archive/compression"
	"github.com/containerd/containerd/images"
	"github.com/opencontainers/go-digest"
	spec "github.com/opencontainers/image-spec/specs-go/v1"

	ctdcontainers "github.com/containerd/containerd/containers"
	ctdnamespaces "github.com/containerd/containerd/namespaces"
	ctdoci "github.com/containerd/containerd/oci"
	"github.com/containerd/containerd/platforms"
	runtimespec "github.com/opencontainers/runtime-spec/specs-go"
)

func main() {
	app := cli.NewApp()
	app.Name = "d2oci"
	app.Version = version.Version
	app.Usage = "docker container to oci"
	app.UsageText = fmt.Sprintf("%s [options] image-name [output]", app.Name)
	app.ArgsUsage = "image-name [output]"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "builder",
			Usage: "Bulider command to use",
			Value: "docker",
		},
		cli.StringFlag{
			Name:  "target-arch",
			Usage: "target architecture of the source image to use",
			Value: "amd64",
		},
		cli.StringFlag{
			Name:  "rootfs-type",
			Usage: "file system used for rootfs",
			Value: "ero",
		},
	}
	app.Action = rootAction
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%+v\n", err)
		os.Exit(1)
	}
}

func rootAction(clicontext *cli.Context) error {
	srcImgName := clicontext.Args().First()
	if srcImgName == "" {
		return fmt.Errorf("specify image name")
	}

	dstImgPath := clicontext.Args().Get(1)
	if dstImgPath == "" {
		return fmt.Errorf("specify output destination")
	}

	ociDstImgPath := path.Join(dstImgPath, "rootfs")

	arch := clicontext.String("target-arch")
	if arch == "" {
		arch = "amd64"
	}

	fstype := clicontext.String("rootfs-type")
	if fstype == "" {
		fstype = "ero"
	}

	builderPath, err := exec.LookPath(clicontext.String("builder"))
	if err != nil {
		return err
	}

	os.Mkdir(dstImgPath, 0750)
	err = os.RemoveAll(dstImgPath + "/rootfs")
	if err != nil {
		return err
	}
	os.Mkdir(dstImgPath+"/rootfs", 0750)

	if err := prepareSourceImg(builderPath, srcImgName, dstImgPath, arch); err != nil {
		return fmt.Errorf("failed to prepare image: %w", err)
	}

	platform, err := platforms.Parse(arch)
	if err != nil {
		return err
	}

	_, err = unpack(context.TODO(), dstImgPath, &platform, ociDstImgPath)
	if err != nil {
		return err
	}

	// Fetch index
	indexJsonFile, err := os.Open(dstImgPath + "/index.json")
	if err != nil {
		return err
	}
	defer indexJsonFile.Close()

	var index spec.Index
	if err := json.NewDecoder(indexJsonFile).Decode(&index); err != nil {
		return err
	}

	var target digest.Digest
	found := false
	for _, m := range index.Manifests {
		p := platform
		if m.Platform != nil {
			p = *m.Platform
		}
		if platforms.NewMatcher(platform).Match(p) {
			target, found = m.Digest, true
			break
		}
	}
	if !found {
		return fmt.Errorf("manifest not found for platform %v", platform)
	}

	// Fetch manifest
	manifestFile, err := os.Open(dstImgPath + "/blobs/sha256/" + target.Encoded())
	if err != nil {
		return err
	}
	defer manifestFile.Close()

	var manifest spec.Manifest
	if err := json.NewDecoder(manifestFile).Decode(&manifest); err != nil {
		return err
	}

	// Fetch config
	configFile, err := os.Open(dstImgPath + "/blobs/sha256/" + manifest.Config.Digest.Encoded())
	if err != nil {
		return err
	}
	defer configFile.Close()

	var config spec.Image
	configD, err := io.ReadAll(configFile)
	if err != nil {
		return err
	}

	if err := json.NewDecoder(bytes.NewReader(configD)).Decode(&config); err != nil {
		return err
	}

	os.Mkdir(path.Join(dstImgPath, "config"), 0755)

	if err := os.WriteFile(path.Join(dstImgPath, "config/imageconfig.json"), configD, 0640); err != nil {
		return err
	}

	s, err := generateSpec(config)
	if err != nil {
		return err
	}
	specD, err := json.Marshal(s)
	if err != nil {
		return err
	}

	if err := os.WriteFile(path.Join(dstImgPath, "config/config.json"), specD, 0640); err != nil {
		return err
	}

	switch fstype {
	case "ero":
		return createEroFSFromFolder(path.Join(dstImgPath, "rootfs"), path.Join(dstImgPath, "rootfs.bin"))
	case "squash":
		return createSquashFSFromFolder(path.Join(dstImgPath, "rootfs"), path.Join(dstImgPath, "rootfs.bin"))
	default:
		return fmt.Errorf("%s is not a valid entry for rootfs filesystem", fstype)
	}
}

func createSquashFSFromFolder(folderPath, output string) error {
	os.Remove(output)

	// Create the tar command
	tarCmd := exec.Command("tar", "--numeric-owner", "--owner=0", "--group=0", "-C", folderPath, "-cf", "-", ".")

	// Create the mksquashfs command
	mksquashfsCmd := exec.Command("sqfstar", output)

	// Set up the pipe
	r, w := io.Pipe()
	mksquashfsCmd.Stdin, tarCmd.Stdout = r, w

	// Run the tar command
	if err := tarCmd.Start(); err != nil {
		return fmt.Errorf("error running tar command: %w", err)
	}

	// Start the mksquashfs command first
	if err := mksquashfsCmd.Start(); err != nil {
		return fmt.Errorf("error starting mksquashfs command: %w", err)
	}

	// Wait for mksquashfs to finish
	if err := tarCmd.Wait(); err != nil {
		return fmt.Errorf("error waiting for mksquashfs command to finish: %w", err.(*exec.ExitError))
	}
	w.Close()

	if err := mksquashfsCmd.Wait(); err != nil {
		return fmt.Errorf("error waiting for mksquashfs command to finish: %w", err.(*exec.ExitError))
	}
	r.Close()

	return nil
}

func createEroFSFromFolder(folderPath, output string) error {
	os.Remove(output)

	// Create the mkfs.erofs command
	mkfsCmd := exec.Command("mkfs.erofs", "--all-root", output, folderPath)

	// Start the mksquashfs command first
	if err := mkfsCmd.Run(); err != nil {
		return fmt.Errorf("error starting mkfs.erofs command: %w", err)
	}

	return nil
}

func generateSpec(config spec.Image) (_ *runtimespec.Spec, err error) {
	ic := config.Config
	ctdCtx := ctdnamespaces.WithNamespace(context.TODO(), "default")
	p := "linux/riscv64"
	if config.Architecture == "amd64" {
		p = "linux/amd64"
	}
	s, err := ctdoci.GenerateSpecWithPlatform(ctdCtx, nil, p, &ctdcontainers.Container{},
		ctdoci.WithHostNamespace(runtimespec.NetworkNamespace),
		ctdoci.WithoutRunMount,
		ctdoci.WithEnv(ic.Env),
		ctdoci.WithTTY, // TODO: make it configurable
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate spec: %w", err)
	}

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

	// TODO: support seccomp and apparmor
	s.Linux.Seccomp = nil
	s.Root = &runtimespec.Root{
		Path: "/run/rootfs",
	}
	// TODO: ports
	return s, nil
}

func unpack(ctx context.Context, imgDir string, platform *spec.Platform, rootfs string) (io.Reader, error) {
	if rootfs == "" {
		return nil, fmt.Errorf("specify rootfs")
	}
	idxR, err := os.Open(filepath.Join(imgDir, "index.json"))
	if err != nil {
		return nil, fmt.Errorf("Failed to unpack the image as an OCI image:", err)
	}
	var idx spec.Index
	if err := json.NewDecoder(idxR).Decode(&idx); err != nil {
		return nil, err
	}
	var platformMC platforms.MatchComparer
	if platform != nil {
		platformMC = platforms.Only(*platform)
	}
	return unpackOCI(ctx, imgDir, platformMC, rootfs, idx.Manifests)
}

func unpackOCI(ctx context.Context, imgDir string, platformMC platforms.MatchComparer, rootfs string, descs []spec.Descriptor) (io.Reader, error) {
	var children []spec.Descriptor
	for _, desc := range descs {
		switch desc.MediaType {
		case spec.MediaTypeImageManifest, images.MediaTypeDockerSchema2Manifest:
			if desc.Platform != nil && platformMC != nil && !platformMC.Match(*desc.Platform) {
				continue
			}
			mfstD, err := os.ReadFile(filepath.Join(imgDir, "/blobs/sha256", desc.Digest.Encoded()))
			if err != nil {
				return nil, err
			}
			var manifest spec.Manifest
			if err := json.Unmarshal(mfstD, &manifest); err != nil {
				return nil, err
			}
			if !isContainerManifest(manifest) {
				fmt.Printf("%v is not a container manifest. skipping...", desc.Digest.String())
				continue
			}
			configD, err := os.ReadFile(filepath.Join(imgDir, "/blobs/sha256", manifest.Config.Digest.Encoded()))
			if err != nil {
				return nil, err
			}
			var image spec.Image
			if err := json.Unmarshal(configD, &image); err != nil {
				return nil, err
			}
			if platformMC != nil && !platformMC.Match(platforms.Normalize(spec.Platform{OS: image.OS, Architecture: image.Architecture})) {
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
					if _, err := archive.Apply(ctx, rootfs, r, archive.WithNoSameOwner()); err != nil {
						return err
					}
					return nil
				}(); err != nil {
					return nil, err
				}
			}
			return bytes.NewReader(configD), nil
		case images.MediaTypeDockerSchema2ManifestList, spec.MediaTypeImageIndex:
			idxD, err := os.ReadFile(filepath.Join(imgDir, "/blobs/sha256", desc.Digest.Encoded()))
			if err != nil {
				return nil, err
			}
			var idx spec.Index
			if err := json.Unmarshal(idxD, &idx); err != nil {
				return nil, err
			}
			children = append(children, idx.Manifests...)
		default:
			return nil, fmt.Errorf("unsupported mediatype %v", desc.MediaType)
		}
	}
	if len(children) > 0 {
		var childrenDescs []spec.Descriptor
		for _, d := range children {
			if d.Platform != nil && platformMC != nil && !platformMC.Match(*d.Platform) {
				continue
			}
			childrenDescs = append(childrenDescs, d)
		}
		sort.SliceStable(childrenDescs, func(i, j int) bool {
			if childrenDescs[i].Platform == nil {
				return false
			}
			if childrenDescs[j].Platform == nil {
				return true
			}
			if platformMC != nil {
				return platformMC.Less(*childrenDescs[i].Platform, *childrenDescs[j].Platform)
			}
			return true
		})
		children = childrenDescs
	}
	if len(children) > 0 {
		fmt.Printf("nested manifest: processing %v\n", children)
		return unpackOCI(ctx, imgDir, platformMC, rootfs, children)
	}
	return nil, fmt.Errorf("target config not found")
}

func isContainerManifest(manifest spec.Manifest) bool {
	if !images.IsConfigType(manifest.Config.MediaType) {
		return false
	}
	for _, desc := range manifest.Layers {
		if !images.IsLayerType(desc.MediaType) {
			return false
		}
	}
	return true
}

func prepareSourceImg(builderPath, imgName, tmpdir, targetarch string) error {
	log.Printf("saving %q to %q\n", imgName, tmpdir)
	// TODO: check architecture
	needsPull := false
	if idata, err := exec.Command(builderPath, "image", "inspect", imgName).Output(); err != nil {
		needsPull = true
	} else if targetarch != "" {
		inspectData := make([]map[string]interface{}, 1)
		if err := json.Unmarshal(idata, &inspectData); err != nil {
			return err
		}
		if a := inspectData[0]["Architecture"]; a != targetarch {
			log.Printf("unexpected archtecture %v (target: %v). Try \"--target-arch\" when specifying an architecture.\n", a, targetarch)
			needsPull = true
		}
	}
	if needsPull {
		args := []string{"pull"}
		if targetarch != "" {
			args = append(args, "--platform=linux/"+targetarch)
		}
		args = append(args, imgName)
		log.Printf("cannot get image %q locally; pulling it from the registry...\n", imgName)
		pullCmd := exec.Command(builderPath, args...)
		pullCmd.Stdout = os.Stdout
		pullCmd.Stderr = os.Stderr
		if err := pullCmd.Run(); err != nil {
			return fmt.Errorf("failed to pull the image. Try \"--target-arch\" when specifying an architecture: %w", err)
		}
	}
	saveCmd := exec.Command(builderPath, "save", imgName)
	outR, err := saveCmd.StdoutPipe()
	if err != nil {
		return err
	}
	defer outR.Close()
	saveCmd.Stderr = os.Stderr
	if err := saveCmd.Start(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	if _, err := archive.Apply(ctx, tmpdir, outR, archive.WithNoSameOwner()); err != nil {
		return err
	}
	if err := saveCmd.Wait(); err != nil {
		return err
	}

	// update timestamp so that BuildKit can prioritize them over cached files
	now := time.Now().Local()
	return filepath.Walk(tmpdir, func(p string, info fs.FileInfo, err error) error {
		return os.Chtimes(p, now, now)
	})
}
