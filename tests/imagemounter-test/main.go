package main

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"compress/gzip"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental/sock"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	digest "github.com/opencontainers/go-digest"
)

func main() {
	var (
		stack     = flag.String("stack", "", "path to the imagemoutner wasm path")
		stackPort = flag.Int("stack-port", 8999, "listen port of network stack")
		vmPort    = flag.Int("vm-port", 1234, "listen port of vm")
		debug     = flag.Bool("debug", false, "enable debug log")
		imageAddr     = flag.String("image", "", "address of image to run")
	)
	var envs envFlags
	flag.Var(&envs, "env", "environment variables")
	flag.Parse()
	if *debug {
		log.SetOutput(os.Stdout)
	} else {
		log.SetOutput(io.Discard)
	}
	if *imageAddr == "" {
		panic("specify container image")
	}
	args := flag.Args()

	if stack == nil || *stack == "" {
		panic("specify network stack wasm")
	}

	crtDir, err := os.MkdirTemp("", "test")
	if err != nil {
		panic(err)
	}
	defer func() {
		os.Remove(filepath.Join(crtDir, "ca.crt"))
		os.Remove(crtDir)
	}()

	go func() {
		ctx := context.Background()
		sockCfg := sock.NewConfig().WithTCPListener("127.0.0.1", *stackPort)
		ctx = sock.WithConfig(ctx, sockCfg)
		c, err := os.ReadFile(*stack)
		if err != nil {
			panic(err)
		}
		r := wazero.NewRuntime(ctx)
		defer func() {
			r.Close(ctx)
		}()
		wasi_snapshot_preview1.MustInstantiate(ctx, r)
		b := r.NewHostModuleBuilder("env").
			NewFunctionBuilder().WithFunc(http_send).Export("http_send").
			NewFunctionBuilder().WithFunc(http_writebody).Export("http_writebody").
			NewFunctionBuilder().WithFunc(http_isreadable).Export("http_isreadable").
			NewFunctionBuilder().WithFunc(http_recv).Export("http_recv").
			NewFunctionBuilder().WithFunc(http_readbody).Export("http_readbody").
			NewFunctionBuilder().WithFunc(layer_request).Export("layer_request").
			NewFunctionBuilder().WithFunc(layer_isreadable).Export("layer_isreadable").
			NewFunctionBuilder().WithFunc(layer_readat).Export("layer_readat")
		if _, err := b.Instantiate(ctx); err != nil {
			panic(err)
		}
		compiled, err := r.CompileModule(ctx, c)
		if err != nil {
			panic(err)
		}
		stackFSConfig := wazero.NewFSConfig()
		stackFSConfig = stackFSConfig.WithDirMount(crtDir, "/test")
		flagargs := []string{"--certfile=/test/proxy.crt", "--image-addr="+*imageAddr}
		if *debug {
			flagargs = append(flagargs, "--debug")
		}
		conf := wazero.NewModuleConfig().WithSysWalltime().WithSysNanotime().WithSysNanosleep().WithRandSource(crand.Reader).WithStdout(os.Stdout).WithStderr(os.Stderr).WithFSConfig(stackFSConfig).WithArgs(append([]string{"arg0"}, flagargs...)...)
		_, err = r.InstantiateModule(ctx, compiled, conf)
		if err != nil {
			panic(err)
		}
	}()
	go func() {
		var conn1 net.Conn
		var conn2 net.Conn
		var err error
		for {
			conn1, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", *stackPort))
			if err == nil {
				break
			}
			log.Println("conn1 retry...")
			time.Sleep(10 * time.Millisecond)
		}
		for {
			conn2, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", *vmPort))
			if err == nil {
				break
			}
			log.Println("conn2 retry...")
			time.Sleep(10 * time.Millisecond)
		}
		done1 := make(chan struct{})
		done2 := make(chan struct{})
		go func() {
			if _, err := io.Copy(conn1, conn2); err != nil {
				panic(err)
			}
			close(done1)
		}()
		go func() {
			if _, err := io.Copy(conn2, conn1); err != nil {
				panic(err)
			}
			close(done2)
		}()
		<-done1
		<-done2
	}()

	fsConfig := wazero.NewFSConfig()
	crtFile := filepath.Join(crtDir, "proxy.crt")
	for {
		if _, err := os.Stat(crtFile); err == nil {
			break
		}
		log.Println("waiting for cert", crtFile)
		time.Sleep(time.Second)
	}

	fsConfig = fsConfig.WithDirMount(crtDir, "/.wasmenv")
	ctx := context.Background()
	sockCfg := sock.NewConfig().WithTCPListener("127.0.0.1", *vmPort)
	ctx = sock.WithConfig(ctx, sockCfg)
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
	envs = append(envs, []string{
		"SSL_CERT_FILE=/.wasmenv/proxy.crt",
		"https_proxy=http://192.168.127.253:80",
		"http_proxy=http://192.168.127.253:80",
		"HTTPS_PROXY=http://192.168.127.253:80",
		"HTTP_PROXY=http://192.168.127.253:80",
	}...)
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

var respMap = make(map[uint32]*http.Response)
var respMapMu sync.Mutex

var respRMap = make(map[uint32]io.Reader)
var respRMapMu sync.Mutex

var layerMap = make(map[uint32][]byte)
var layerMapMu sync.Mutex

type fetchParameters struct {
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

var reqMap = make(map[uint32]*io.PipeWriter)
var reqMapMu sync.Mutex
var reqMapId uint32

const ERRNO_INVAL = 28

func layer_request(ctx context.Context, m api.Module, addressP uint32, addresslen uint32, digestP uint32, digestlen uint32, withDecompression uint32, idP uint32) uint32 {
	mem := m.Memory()

	addressB, ok := mem.Read(addressP, uint32(addresslen))
	if !ok {
		log.Println("failed to get layer address")
		return ERRNO_INVAL
	}
	address := string(addressB)

	digestB, ok := mem.Read(digestP, uint32(digestlen))
	if !ok {
		log.Println("failed to get layer address")
		return ERRNO_INVAL
	}

	req, err := http.NewRequest("GET", address, nil)
	if err != nil {
		log.Println("failed to create layer req:", err)
		return ERRNO_INVAL
	}

	reqMapMu.Lock()
	id := reqMapId
	reqMapId++
	reqMapMu.Unlock()
	go func() {
		c := &http.Client{}
		resp, err := c.Do(req)
		if err != nil {
			log.Println("failed to do layer request:", err)
			return
		}
		defer resp.Body.Close()
		respMapMu.Lock()
		respMap[id] = resp
		respMapMu.Unlock()

		v := digest.Digest("sha256:"+string(digestB)).Verifier()
		r := io.TeeReader(resp.Body, v)
		if withDecompression == 1 {
			raw := r
			zr, err := gzip.NewReader(raw)
			if err != nil {
				log.Println("failed to prepare layer decompressor:", err)
				return
			}
			defer zr.Close()
			r = zr
		}
		data, err := io.ReadAll(r)
		if err != nil {
			log.Println("failed to read layer:", err)
			return
		}
		if !v.Verified() {
			log.Println("failed to veriy layer (%q)", string(digestB))
			return
		}
		layerMapMu.Lock()
		layerMap[id] = data
		layerMapMu.Unlock()
	}()
	if !mem.WriteUint32Le(idP, id) {
		log.Println("failed to pass id")
		return ERRNO_INVAL
	}
	return 0
}

func layer_isreadable(ctx context.Context, m api.Module, id uint32, isOKP uint32, sizeP uint32) uint32 {
	mem := m.Memory()
	layerMapMu.Lock()
	buf, ok := layerMap[id]
	layerMapMu.Unlock()
	v := uint32(0)
	if ok {
		v = 1
		if !mem.WriteUint32Le(sizeP, uint32(len(buf))) {
			log.Println("failed to pass status")
			return ERRNO_INVAL
		}
	}
	if !mem.WriteUint32Le(isOKP, v) {
		log.Println("failed to pass status")
		return ERRNO_INVAL
	}
	return 0
}

func layer_readat(ctx context.Context, m api.Module, id uint32, respP uint32, offset uint32, wantlen uint32, respsizeP uint32) uint32 {
	mem := m.Memory()
	layerMapMu.Lock()
	buf, ok := layerMap[id]
	layerMapMu.Unlock()
	if !ok {
		log.Printf("no layer found for id %d\n", id)
		return ERRNO_INVAL
	}

	if uint32(len(buf)) < offset + wantlen {
		wantlen = uint32(len(buf)) - offset
	}
	if !mem.Write(respP, buf[offset:offset+wantlen]) {
		log.Println("failed to write layer")
		return ERRNO_INVAL
	}
	if !mem.WriteUint32Le(respsizeP, uint32(wantlen)) {
		log.Println("failed to write resp body size")
		return ERRNO_INVAL
	}
	return 0
}


func http_send(ctx context.Context, m api.Module, addressP uint32, addresslen uint32, reqP, reqlen uint32, idP uint32) uint32 {
	mem := m.Memory()

	addressB, ok := mem.Read(addressP, uint32(addresslen))
	if !ok {
		log.Println("failed to get address")
		return ERRNO_INVAL
	}
	address := string(addressB)

	reqB, ok := mem.Read(reqP, uint32(reqlen))
	if !ok {
		log.Println("failed to get req")
		return ERRNO_INVAL
	}
	var fetchReq fetchParameters
	if err := json.Unmarshal(reqB, &fetchReq); err != nil {
		log.Println("failed to marshal req:", err)
		return ERRNO_INVAL
	}

	pr, pw := io.Pipe()
	req, err := http.NewRequest(fetchReq.Method, address, pr)
	if err != nil {
		log.Println("failed to create req:", err)
		return ERRNO_INVAL
	}
	if req.Header == nil {
		req.Header = make(map[string][]string)
	}
	for k, v := range fetchReq.Headers {
		req.Header.Add(k, v)
	}

	reqMapMu.Lock()
	id := reqMapId
	reqMapId++
	reqMapMu.Unlock()
	reqMap[id] = pw
	go func() {
		c := &http.Client{}
		resp, err := c.Do(req)
		if err != nil {
			log.Println("failed to do request:", err)
			return
		}
		respMapMu.Lock()
		respMap[id] = resp
		respMapMu.Unlock()
	}()
	if !mem.WriteUint32Le(idP, id) {
		log.Println("failed to pass id")
		return ERRNO_INVAL
	}
	return 0
}

func http_writebody(ctx context.Context, m api.Module, id uint32, chunkP, len uint32, nwrittenP uint32, isEOF uint32) uint32 {
	mem := m.Memory()

	chunkB, ok := mem.Read(chunkP, uint32(len))
	if !ok {
		log.Println("failed to get chunk")
		return ERRNO_INVAL
	}
	reqMapMu.Lock()
	w := reqMap[id]
	reqMapMu.Unlock()
	if _, err := w.Write(chunkB); err != nil {
		w.CloseWithError(err)
		log.Println("failed to write req:", err)
		return ERRNO_INVAL
	}
	if isEOF == 1 {
		w.Close()
	}
	if !mem.WriteUint32Le(nwrittenP, len) {
		log.Println("failed to pass written number")
		return ERRNO_INVAL
	}
	return 0
}

func http_isreadable(ctx context.Context, m api.Module, id uint32, isOKP uint32) uint32 {
	mem := m.Memory()
	respMapMu.Lock()
	_, ok := respMap[id]
	respMapMu.Unlock()
	v := uint32(0)
	if ok {
		v = 1
	}
	if !mem.WriteUint32Le(isOKP, v) {
		log.Println("failed to pass status")
		return ERRNO_INVAL
	}
	return 0
}

func http_recv(ctx context.Context, m api.Module, id uint32, respP uint32, bufsize uint32, respsizeP uint32, isEOFP uint32) uint32 {
	mem := m.Memory()
	respRMapMu.Lock()
	respR, ok := respRMap[id]
	respRMapMu.Unlock()
	if !ok {
		respMapMu.Lock()
		resp, ok := respMap[id]
		respMapMu.Unlock()
		if !ok {
			log.Println("failed to get resp")
			return ERRNO_INVAL
		}
		var fetchResp struct {
			Headers    map[string]string `json:"headers,omitempty"`
			Status     int               `json:"status,omitempty"`
			StatusText string            `json:"statusText,omitempty"`
		}
		fetchResp.Headers = make(map[string]string)
		for k, v := range resp.Header {
			fetchResp.Headers[k] = strings.Join(v, ", ")
		}
		fetchResp.Status = resp.StatusCode
		fetchResp.StatusText = resp.Status
		respD, err := json.Marshal(fetchResp)
		if err != nil {
			log.Println("failed to marshal resp:", err)
			return ERRNO_INVAL
		}
		respR = bytes.NewReader(respD)
		respRMapMu.Lock()
		respRMap[id] = respR
		respRMapMu.Unlock()
	}

	isEOF := uint32(0)
	buf := make([]byte, bufsize)
	n, err := respR.Read(buf)
	if err != nil && err != io.EOF {
		log.Println("failed to read resp:", err)
		return ERRNO_INVAL
	} else if err == io.EOF {
		isEOF = 1
	}
	if !mem.Write(respP, buf[:n]) {
		log.Println("failed to write resp")
		return ERRNO_INVAL
	}
	if !mem.WriteUint32Le(respsizeP, uint32(n)) {
		log.Println("failed to write resp size")
		return ERRNO_INVAL
	}
	if !mem.WriteUint32Le(isEOFP, isEOF) {
		log.Println("failed to write EOF status")
		return ERRNO_INVAL
	}
	return 0
}

func http_readbody(ctx context.Context, m api.Module, id uint32, bodyP uint32, bufsize uint32, bodysizeP uint32, isEOFP uint32) uint32 {
	mem := m.Memory()
	respMapMu.Lock()
	resp, ok := respMap[id]
	respMapMu.Unlock()
	if !ok {
		log.Println("failed to get resp")
		return ERRNO_INVAL
	}

	isEOF := uint32(0)
	buf := make([]byte, bufsize)
	n, err := resp.Body.Read(buf)
	if err != nil && err != io.EOF {
		log.Println("failed to read resp body:", err)
		return ERRNO_INVAL
	} else if err == io.EOF {
		isEOF = 1
	}
	if !mem.Write(bodyP, buf[:n]) {
		log.Println("failed to write resp body")
		return ERRNO_INVAL
	}
	if !mem.WriteUint32Le(bodysizeP, uint32(n)) {
		log.Println("failed to write resp body size")
		return ERRNO_INVAL
	}
	if !mem.WriteUint32Le(isEOFP, isEOF) {
		log.Println("failed to write resp body EOF status")
		return ERRNO_INVAL
	}
	return 0
}

type envFlags []string

func (i *envFlags) String() string {
	return fmt.Sprintf("%v", []string(*i))
}
func (i *envFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}
