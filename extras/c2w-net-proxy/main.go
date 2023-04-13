package main

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"

	gvntypes "github.com/containers/gvisor-tap-vsock/pkg/types"
	gvnvirtualnetwork "github.com/containers/gvisor-tap-vsock/pkg/virtualnetwork"
	"github.com/sirupsen/logrus"
)

var proxyKey crypto.PrivateKey
var proxyCert *x509.Certificate

const proxyIP = "192.168.127.253"

func handleTunneling(w http.ResponseWriter, r *http.Request) {
	serverURL := r.URL
	cert, err := generateCert(serverURL.Hostname())
	if err != nil {
		log.Printf("failed to generate cert: %v\n", err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}
	client_conn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	go func() {
		defer client_conn.Close()
		l := newListener(&stringAddr{"tcp", proxyIP + ":80"}, client_conn)
		server := &http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Scheme == "" {
					r.URL.Scheme = "https"
				}
				if r.URL.Host == "" {
					r.URL.Host = serverURL.Host
				}
				handleHTTP(w, r)
			}),
			// Disable HTTP/2 (FIXME)
			// https://pkg.go.dev/net/http#hdr-HTTP_2
			TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		}
		server.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{*cert},
		}
		log.Printf("serving server for %s...\n", serverURL.Host)
		server.ServeTLS(l, "", "")
	}()
}

func generateCert(host string) (*tls.Certificate, error) {
	ser, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 60))
	if err != nil {
		return nil, err
	}
	cert := &x509.Certificate{
		SerialNumber: ser,
		Subject: pkix.Name{
			CommonName: host,
		},
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{host},
	}
	if ip := net.ParseIP(host); ip != nil {
		cert.IPAddresses = append(cert.IPAddresses, ip)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	certD, err := x509.CreateCertificate(rand.Reader, cert, proxyCert, key.Public(), proxyKey)
	if err != nil {
		return nil, err
	}
	return &tls.Certificate{
		Certificate: [][]byte{certD},
		PrivateKey:  key,
	}, nil
}

type listener struct {
	ch      net.Conn
	addr    net.Addr
	closeCh chan struct{}
}

func newListener(addr net.Addr, ch net.Conn) *listener {
	return &listener{
		ch:      ch,
		addr:    addr,
		closeCh: make(chan struct{}),
	}
}

func (l *listener) Accept() (net.Conn, error) {
	if l.ch == nil {
		<-l.closeCh
		return nil, fmt.Errorf("closed")
	}
	c := l.ch
	l.ch = nil
	return c, nil
}

func (l *listener) Close() error { close(l.closeCh); return nil }

func (l *listener) Addr() net.Addr { return l.addr }

type stringAddr struct {
	network string
	address string
}

func (a *stringAddr) Network() string { return a.network }
func (a *stringAddr) String() string  { return a.address }

type FetchParameters struct {
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type FetchResponse struct {
	Headers    map[string]string `json:"headers,omitempty"`
	Status     int               `json:"status,omitempty"`
	StatusText string            `json:"statusText,omitempty"`
}

func encodeHeader(h http.Header) map[string]string {
	res := make(map[string]string)
	for k, vs := range h {
		res[k] = strings.Join(vs, ", ")
	}
	return res
}

func decodeHeader(h map[string]string) http.Header {
	res := make(map[string][]string)
	for k, v := range h {
		res[k] = []string{v} // TODO: separate fields
	}
	return http.Header(res)
}

func httpRequestToFetchParameters(req *http.Request) *FetchParameters {
	return &FetchParameters{
		Method:  req.Method,
		Headers: encodeHeader(req.Header),
	}
}

func fetchResponseToHTTPResponse(resp *FetchResponse) *http.Response {
	return &http.Response{
		Status:     resp.StatusText,
		StatusCode: resp.Status,
		Header:     decodeHeader(resp.Headers),
	}
}

//go:wasmimport env http_send
func http_send(addressP uint32, addresslen uint32, reqP, reqlen uint32, idP uint32) uint32

//go:wasmimport env http_writebody
func http_writebody(id uint32, chunkP, len uint32, nwrittenP uint32, isEOF uint32) uint32

//go:wasmimport env http_isreadable
func http_isreadable(id uint32, isOKP uint32) uint32

//go:wasmimport env http_recv
func http_recv(id uint32, respP uint32, bufsize uint32, respsizeP uint32, isEOFP uint32) uint32

//go:wasmimport env http_readbody
func http_readbody(id uint32, bodyP uint32, bufsize uint32, bodysizeP uint32, isEOFP uint32) uint32

func doHttpRoundTrip(req *http.Request) (*http.Response, error) {
	defer req.Body.Close()

	address := req.URL.String()
	if address == "" {
		return nil, fmt.Errorf("specify destination address")
	}

	fetchReqD, err := json.Marshal(httpRequestToFetchParameters(req))
	if err != nil {
		return nil, err
	}
	if len(fetchReqD) == 0 {
		return nil, fmt.Errorf("empty request")
	}

	var id uint32
	res := http_send(
		uint32(uintptr(unsafe.Pointer(&[]byte(address)[0]))),
		uint32(len(address)),
		uint32(uintptr(unsafe.Pointer(&[]byte(fetchReqD)[0]))),
		uint32(len(fetchReqD)),
		uint32(uintptr(unsafe.Pointer(&id))),
	)
	if res != 0 {
		return nil, fmt.Errorf("failed to send request")
	}

	var isEOF uint32
	var reqBodyD []byte = make([]byte, 4096)
	var nwritten uint32 = 0
	idx := 0
	chunksize := 0
	for {
		if idx >= chunksize {
			// chunk is fully written. full another one.
			chunksize, err = req.Body.Read(reqBodyD)
			if err != nil && err != io.EOF {
				return nil, err
			}
			if err == io.EOF {
				isEOF = 1
			}
			idx = 0
		}
		res := http_writebody(
			id,
			uint32(uintptr(unsafe.Pointer(&[]byte(reqBodyD[idx:])[0]))),
			uint32(chunksize),
			uint32(uintptr(unsafe.Pointer(&nwritten))),
			isEOF,
		)
		if res != 0 {
			return nil, fmt.Errorf("failed to write request body")
		}
		idx += int(nwritten)
		if idx < chunksize {
			// not fully written. retry for the remaining.
			continue
		}
		if isEOF == 1 {
			break
		}
	}

	var isOK uint32 = 0
	for {
		res := http_isreadable(id, uint32(uintptr(unsafe.Pointer(&isOK))))
		if res != 0 {
			return nil, fmt.Errorf("response body is not readable")
		}
		if isOK == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	var respD []byte = make([]byte, 4096)
	var respsize uint32
	var respFull []byte
	isEOF = 0
	for {
		res := http_recv(
			id,
			uint32(uintptr(unsafe.Pointer(&[]byte(respD)[0]))),
			4096,
			uint32(uintptr(unsafe.Pointer(&respsize))),
			uint32(uintptr(unsafe.Pointer(&isEOF))),
		)
		if res != 0 {
			return nil, fmt.Errorf("failed to receive response")
		}
		respFull = append(respFull, respD[:int(respsize)]...)
		if isEOF == 1 {
			break
		}
	}
	var resp FetchResponse
	if err := json.Unmarshal(respFull, &resp); err != nil {
		return nil, err
	}

	isEOF = 0
	pr, pw := io.Pipe()
	go func() {
		var body []byte = make([]byte, 4096)
		var bodysize uint32
		for {
			res := http_readbody(
				id,
				uint32(uintptr(unsafe.Pointer(&[]byte(body)[0]))),
				4096,
				uint32(uintptr(unsafe.Pointer(&bodysize))),
				uint32(uintptr(unsafe.Pointer(&isEOF))),
			)
			if res != 0 {
				pw.CloseWithError(fmt.Errorf("failed to read response body"))
				return
			}
			if bodysize > 0 {
				if _, err := pw.Write(body[:int(bodysize)]); err != nil {
					pw.CloseWithError(err)
					return
				}
			}
			if isEOF == 1 {
				break
			}
		}
		pw.Close()
	}()
	r := fetchResponseToHTTPResponse(&resp)
	r.Body = pr
	return r, nil
}

func handleHTTP(w http.ResponseWriter, req *http.Request) {
	resp, err := doHttpRoundTrip(req)
	if err != nil {
		log.Printf("failed to proxy request: %v\n", err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

const (
	gatewayIP = "192.168.127.1"
	vmIP      = "192.168.127.3"
)

func main() {
	var listenFd int
	flag.IntVar(&listenFd, "net-listenfd", 0, "fd to listen for the connection")
	var certFd int
	flag.IntVar(&certFd, "certfd", 0, "fd to output cert")
	var certFile string
	flag.StringVar(&certFile, "certfile", "", "file to output cert")
	var debug bool
	flag.BoolVar(&debug, "debug", false, "debug log")
	flag.Parse()

	if debug {
		log.SetOutput(os.Stdout)
		logrus.SetLevel(logrus.DebugLevel)
	} else {
		log.SetOutput(io.Discard)
		logrus.SetLevel(logrus.FatalLevel)
	}

	ser, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 60))
	if err != nil {
		panic(err)
	}
	cert := &x509.Certificate{
		SerialNumber: ser,
		Subject: pkix.Name{
			CommonName:         proxyIP,
			OrganizationalUnit: []string{"proxy"},
			Organization:       []string{"proxy"},
			Country:            []string{"US"},
		},
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{proxyIP},
		IPAddresses:           []net.IP{net.ParseIP(proxyIP)},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	certD, err := x509.CreateCertificate(rand.Reader, cert, cert, key.Public(), key)
	if err != nil {
		panic(err)
	}
	var f io.WriteCloser
	if certFd != 0 {
		f = os.NewFile(uintptr(certFd), "")
	} else if certFile != "" {
		var err error
		f, err = os.OpenFile(certFile, os.O_RDWR|os.O_CREATE, 0755)
		if err != nil {
			panic(err)
		}
	} else {
		panic("specify cert destination")
	}
	if err := pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: certD}); err != nil {
		panic(err)
	}
	if err := f.Close(); err != nil {
		panic(err)
	}
	tlsCert := &tls.Certificate{
		Certificate: [][]byte{certD},
		PrivateKey:  key,
	}
	proxyCert, err = x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		panic(err)
	}
	proxyKey = key

	config := &gvntypes.Configuration{
		Debug:             debug,
		MTU:               1500,
		Subnet:            "192.168.127.0/24",
		GatewayIP:         gatewayIP,
		GatewayMacAddress: "5a:94:ef:e4:0c:dd",
		GatewayVirtualIPs: []string{proxyIP},
		Protocol:          gvntypes.QemuProtocol,
	}
	vn, err := gvnvirtualnetwork.New(config)
	if err != nil {
		panic(err)
	}

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodConnect {
				handleTunneling(w, r)
			} else {
				handleHTTP(w, r)
			}
		}),
		// Disable HTTP/2 (FIXME)
		// https://pkg.go.dev/net/http#hdr-HTTP_2
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	go func() {
		s := server
		s.Addr = "0.0.0.0:80"
		l, err := vn.Listen("tcp", proxyIP+":80")
		if err != nil {
			panic(err)
		}
		log.Println("serving proxy with http")
		log.Fatal(server.Serve(l))
	}()
	go func() {
		s := server
		s.Addr = "0.0.0.0:443"
		l, err := vn.Listen("tcp", proxyIP+":443")
		if err != nil {
			panic(err)
		}
		s.TLSConfig = &tls.Config{
			Certificates: make([]tls.Certificate, 1),
		}
		s.TLSConfig.Certificates[0] = *tlsCert
		log.Println("serving proxy with https")
		log.Fatal(server.ServeTLS(l, "", ""))
	}()
	ql, err := findListener(listenFd)
	if err != nil {
		panic(err)
	}
	if ql == nil {
		panic("socket fd not found")
	}
	qconn, err := ql.Accept()
	if err != nil {
		panic(err)
	}
	if err := vn.AcceptQemu(context.TODO(), qconn); err != nil {
		panic(err)
	}
}

func findListener(listenFd int) (net.Listener, error) {
	if listenFd == 0 {
		for preopenFd := 3; ; preopenFd++ {
			var stat syscall.Stat_t
			if err := syscall.Fstat(preopenFd, &stat); err != nil {
				var se syscall.Errno
				if errors.As(err, &se) && se == syscall.EBADF {
					err = nil
				}
				log.Printf("findListner failed (fd=%d): %v\n", preopenFd, err)
				return nil, err
			} else if stat.Filetype == syscall.FILETYPE_SOCKET_STREAM {
				listenFd = preopenFd
				break
			}
		}
	}
	syscall.SetNonblock(listenFd, true)
	f := os.NewFile(uintptr(listenFd), "")
	defer f.Close()
	log.Printf("using socket at fd=%d\n", listenFd)
	return net.FileListener(f)
}
