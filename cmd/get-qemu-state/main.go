package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"time"
)

const (
	defaultOutputFile = "vm.state"
	defaultWaitString = "=========="
)

func main() {
	var (
		outputFile = flag.String("output", defaultOutputFile, "path to output state file")
		argsJSON   = flag.String("args-json", "", "path to json file containing args")
	)

	flag.Parse()
	args := flag.Args()

	if *outputFile == "" {
		log.Fatalf("output file must not be empty")
	}
	if *argsJSON == "" {
		log.Fatalf("specify args JSON")
	}

	var extraArgs []string
	argsData, err := os.ReadFile(*argsJSON)
	if err != nil {
		log.Fatalf("failed to get args json: %v", err)
	}
	if err := json.Unmarshal(argsData, &extraArgs); err != nil {
		log.Fatalf("failed to parse args json: %v", err)
	}
	log.Println(extraArgs)

	cmd := exec.Command(args[0], extraArgs...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}

	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log.Fatalf("failed to start: %v", err)
	}

	snapshotCh := make(chan struct{})
	doneCh := make(chan struct{})
	go func() {
		<-snapshotCh
		_, err := stdin.Write([]byte{byte(0x01), byte('c')}) // send Ctrl-A C to start the monitor mode
		if err != nil {
			log.Fatalf("failed to start monitor: %v", err)
		}
		for {
			if _, err := io.WriteString(stdin, fmt.Sprintf("migrate file:%s\n", *outputFile)); err != nil {
				log.Fatalf("failed to invoke migrate: %v", err)
			}
			time.Sleep(500 * time.Millisecond)
			if _, err := os.Stat(*outputFile); err == nil {
				break // state file exists
			} else if !errors.Is(err, os.ErrNotExist) {
				log.Fatalf("failed to stat state file: %v", err)
			}
		}
		log.Println("finishing QEMU")
		if _, err := io.WriteString(stdin, "quit\n"); err != nil {
			log.Fatalf("failed to invoke quit: %v", err)
		}
		close(doneCh)
	}()

	go func() {
		p := make([]byte, 1)
		cnt := 0
		for {
			if _, err := stdout.Read(p); err != nil {
				log.Fatalf("failed to read stdout: %v", err)
			}
			if string(p) == "=" {
				cnt++
			} else {
				cnt = 0
			}
			if cnt == 10 {
				log.Println("detected marker")
				break // start snapshotting
			}
			if _, err := os.Stdout.Write(p); err != nil {
				log.Fatalf("failed to copy stdout: %v", err)
			}
		}
		close(snapshotCh)
		if _, err := io.Copy(os.Stdout, stdout); err != nil {
			log.Fatalf("failed to copy stdout: %v", err)
		}
	}()

	<-doneCh

	if err := cmd.Wait(); err != nil {
		log.Fatalf("waiting for qemu: %v", err)
	}
}
