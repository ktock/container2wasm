package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/containerd/containerd/archive"
	vendor "github.com/ktock/container2wasm"
	"github.com/ktock/container2wasm/version"
	"github.com/urfave/cli"
)

const defaultOutputFile = "out.wasm"

func main() {
	app := cli.NewApp()
	app.Name = "c2w"
	app.Version = version.Version
	app.Usage = "container to wasm converter"
	app.UsageText = fmt.Sprintf("%s [options] image-name [output file]", app.Name)
	app.ArgsUsage = "image-name [output-file]"
	var flags []cli.Flag
	app.Flags = append([]cli.Flag{
		cli.StringFlag{
			Name:  "assets",
			Usage: "Custom location of build assets.",
		},
		cli.StringFlag{
			Name:  "dockerfile",
			Usage: "Custom location of Dockerfile (default: embedded to this command)", // TODO: allow to show it
		},
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
		cli.StringSliceFlag{
			Name:  "build-arg",
			Usage: "Additional build arguments",
		},
		cli.BoolFlag{
			Name:  "to-js",
			Usage: "output JS files runnable on the browsers using emscripten",
		},
		cli.BoolFlag{
			Name:  "debug-image",
			Usage: "Enable debug print in the output image",
		},
		cli.BoolFlag{
			Name:  "show-dockerfile",
			Usage: "Show default Dockerfile",
		},
		cli.BoolFlag{
			Name:  "legacy",
			Usage: "Use \"docker build\" instead of buildx (no support for assets flag)",
		},
		cli.BoolFlag{
			Name:  "external-bundle",
			Usage: "Do not embed container image to the Wasm image but mount it during runtime",
		},
	}, flags...)
	app.Action = rootAction
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%+v\n", err)
		os.Exit(1)
	}
}

func rootAction(clicontext *cli.Context) error {
	if clicontext.Bool("show-dockerfile") {
		fmt.Printf("%s", vendor.Dockerfile)
		fmt.Printf("\n")
		return nil
	}
	arg1 := clicontext.Args().First()
	if arg1 == "" {
		if clicontext.Bool("external-bundle") {
			return fmt.Errorf("specify output image name")
		} else {
			return fmt.Errorf("specify image name")
		}
	}
	var outputPath string
	if clicontext.Bool("external-bundle") {
		outputPath = arg1
		if clicontext.Args().Get(1) != "" {
			return fmt.Errorf("command receives only 1 arg (output image path) with external-bundle")
		}
	} else {
		outputPath = clicontext.Args().Get(1)
	}
	builderPath, err := exec.LookPath(clicontext.String("builder"))
	if err != nil {
		return err
	}
	legacy := false
	if err := exec.Command(builderPath, "buildx").Run(); err != nil {
		log.Printf("buildx unavailable. falling back to the normal builder.\n")
		legacy = true
	}
	if clicontext.Bool("legacy") {
		legacy = true
	}
	destDir, destFile := ".", defaultOutputFile
	if clicontext.Bool("to-js") {
		destFile = ""
	}
	if outputPath != "" {
		d, f := filepath.Split(outputPath)
		destDir, err = filepath.Abs(d)
		if err != nil {
			return err
		}
		if f != "" {
			destFile = f
		}
	}
	if clicontext.Bool("to-js") && destFile != "" {
		return fmt.Errorf("output destination must be a slash-terminated directory path when using \"to-js\" option")
	}
	if a := clicontext.String("assets"); a != "" && legacy {
		return fmt.Errorf("\"assets\" unsupported on docker build as of now; install docker buildx instead")
	}

	srcImgName := arg1
	tmpdir, err := os.MkdirTemp("", "container2wasm")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpdir)
	srcImgPath := filepath.Join(tmpdir, "img")
	if err := os.Mkdir(srcImgPath, 0755); err != nil {
		return err
	}
	if !clicontext.Bool("external-bundle") {
		if err := prepareSourceImg(builderPath, srcImgName, srcImgPath, clicontext.String("target-arch")); err != nil {
			return fmt.Errorf("failed to prepare image: %w", err)
		}
	}

	if legacy {
		return buildWithLegacyBuilder(builderPath, srcImgPath, destDir, destFile, clicontext)
	}

	return build(builderPath, srcImgPath, destDir, destFile, clicontext)
}

func build(builderPath string, srcImgPath string, destDir, destFile string, clicontext *cli.Context) error {
	buildxArgs := []string{
		"buildx", "build", "--progress=plain",
		"--build-arg", fmt.Sprintf("TARGETARCH=%s", clicontext.String("target-arch")),
		"--build-arg", fmt.Sprintf("TARGETPLATFORM=linux/%s", clicontext.String("target-arch")),
		"--platform=linux/amd64",
	}
	var dockerfilePath string
	if o := clicontext.String("dockerfile"); o != "" {
		dockerfilePath = o
	} else {
		f, err := os.CreateTemp("", "container2wasm")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if _, err := f.Write(vendor.Dockerfile); err != nil {
			return err
		}
		dockerfilePath = f.Name()
	}
	buildxArgs = append(buildxArgs, "-f", dockerfilePath)
	if o := clicontext.String("assets"); o != "" {
		buildxArgs = append(buildxArgs, "--build-context", fmt.Sprintf("assets=%s", o))
	}
	if clicontext.Bool("to-js") {
		buildxArgs = append(buildxArgs,
			"--target=js",
			"--build-arg", "OPTIMIZATION_MODE=native",
		)
	}
	buildxArgs = append(buildxArgs, "--output", fmt.Sprintf("type=local,dest=%s", destDir))
	if destFile != "" {
		buildxArgs = append(buildxArgs, "--build-arg", fmt.Sprintf("OUTPUT_NAME=%s", destFile))
	}
	linuxLogLevel, initDebug := 0, false
	if clicontext.Bool("debug-image") {
		linuxLogLevel, initDebug = 7, true
	}
	buildxArgs = append(buildxArgs,
		"--build-arg", fmt.Sprintf("LINUX_LOGLEVEL=%d", linuxLogLevel),
		"--build-arg", fmt.Sprintf("INIT_DEBUG=%v", initDebug),
	)
	if clicontext.Bool("external-bundle") {
		buildxArgs = append(buildxArgs, "--build-arg", "EXTERNAL_BUNDLE=true")
	}
	for _, a := range clicontext.StringSlice("build-arg") {
		buildxArgs = append(buildxArgs, "--build-arg", a)
	}
	buildxArgs = append(buildxArgs, srcImgPath)
	log.Printf("buildx args: %+v\n", buildxArgs)

	cmd := exec.Command(builderPath, buildxArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func buildWithLegacyBuilder(builderPath string, srcImgPath, destDir, destFile string, clicontext *cli.Context) error {
	buildArgs := []string{
		"build", "--progress=plain",
		"--platform=linux/amd64",
		"--build-arg", fmt.Sprintf("TARGETARCH=%s", clicontext.String("target-arch")),
	}
	var dockerfilePath string
	if o := clicontext.String("dockerfile"); o != "" {
		dockerfilePath = o
	} else {
		f, err := os.CreateTemp("", "container2wasm")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if _, err := f.Write(vendor.Dockerfile); err != nil {
			return err
		}
		dockerfilePath = f.Name()
	}
	buildArgs = append(buildArgs, "-f", dockerfilePath)
	if clicontext.Bool("to-js") {
		buildArgs = append(buildArgs,
			"--target=js",
			"--build-arg", "OPTIMIZATION_MODE=native",
		)
	}
	buildArgs = append(buildArgs, "--output", fmt.Sprintf("type=local,dest=%s", destDir))
	if destFile != "" {
		buildArgs = append(buildArgs, "--build-arg", fmt.Sprintf("OUTPUT_NAME=%s", destFile))
	}
	linuxLogLevel, initDebug := 0, false
	if clicontext.Bool("debug-image") {
		linuxLogLevel, initDebug = 7, true
	}
	buildArgs = append(buildArgs,
		"--build-arg", fmt.Sprintf("LINUX_LOGLEVEL=%d", linuxLogLevel),
		"--build-arg", fmt.Sprintf("INIT_DEBUG=%v", initDebug),
	)
	if clicontext.Bool("external-bundle") {
		buildArgs = append(buildArgs, "--build-arg", "EXTERNAL_BUNDLE=true")
	}
	for _, a := range clicontext.StringSlice("build-arg") {
		buildArgs = append(buildArgs, "--build-arg", a)
	}
	buildArgs = append(buildArgs, srcImgPath)
	log.Printf("build args: %+v\n", buildArgs)

	cmd := exec.Command(builderPath, buildArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
