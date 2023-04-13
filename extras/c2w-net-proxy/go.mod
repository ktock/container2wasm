module c2w-net-proxy

go 1.21.0

require (
	github.com/containers/gvisor-tap-vsock v0.7.0
	github.com/sirupsen/logrus v1.9.3
)

require (
	github.com/Microsoft/go-winio v0.6.1 // indirect
	github.com/apparentlymart/go-cidr v1.1.0 // indirect
	github.com/google/btree v1.0.1 // indirect
	github.com/google/gopacket v1.1.19 // indirect
	github.com/insomniacslk/dhcp v0.0.0-20220504074936-1ca156eafb9f // indirect
	github.com/miekg/dns v1.1.55 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/u-root/uio v0.0.0-20210528114334-82958018845c // indirect
	golang.org/x/crypto v0.11.0 // indirect
	golang.org/x/mod v0.10.0 // indirect
	golang.org/x/net v0.12.0 // indirect
	golang.org/x/sys v0.10.0 // indirect
	golang.org/x/time v0.0.0-20220210224613-90d013bbcef8 // indirect
	golang.org/x/tools v0.9.3 // indirect
	gvisor.dev/gvisor v0.0.0-20230715022000-fd277b20b8db // indirect
	inet.af/tcpproxy v0.0.0-20220326234310-be3ee21c9fa0 // indirect
)

replace github.com/sirupsen/logrus => github.com/sirupsen/logrus v1.9.3-0.20230531171720-7165f5e779a5

// Patched for enabling to compile it to wasi
replace github.com/insomniacslk/dhcp => github.com/ktock/insomniacslk-dhcp v0.0.0-20230911142651-b86573a014b1

// Patched for enabling to compile it to wasi
replace github.com/u-root/uio => github.com/ktock/u-root-uio v0.0.0-20230911142931-5cf720bc8a29
