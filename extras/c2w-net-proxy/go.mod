module c2w-net-proxy

go 1.22.0

require (
	github.com/containers/gvisor-tap-vsock v0.8.0
	github.com/sirupsen/logrus v1.9.3
)

require (
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/apparentlymart/go-cidr v1.1.0 // indirect
	github.com/areYouLazy/libhosty v1.1.0 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/google/btree v1.1.2 // indirect
	github.com/google/gopacket v1.1.19 // indirect
	github.com/insomniacslk/dhcp v0.0.0-20240710054256-ddd8a41251c9 // indirect
	github.com/miekg/dns v1.1.62 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/qdm12/dns/v2 v2.0.0-rc6 // indirect
	github.com/qdm12/gosettings v0.4.1 // indirect
	github.com/u-root/uio v0.0.0-20240224005618-d2acac8f3701 // indirect
	golang.org/x/crypto v0.28.0 // indirect
	golang.org/x/exp v0.0.0-20240719175910-8a7402abbf56 // indirect
	golang.org/x/mod v0.20.0 // indirect
	golang.org/x/net v0.28.0 // indirect
	golang.org/x/sync v0.8.0 // indirect
	golang.org/x/sys v0.26.0 // indirect
	golang.org/x/time v0.5.0 // indirect
	golang.org/x/tools v0.24.0 // indirect
	gvisor.dev/gvisor v0.0.0-20240916094835-a174eb65023f // indirect
)

replace github.com/sirupsen/logrus => github.com/sirupsen/logrus v1.9.3-0.20230531171720-7165f5e779a5

// Patched for enabling to compile it to wasi
replace github.com/insomniacslk/dhcp => github.com/ktock/insomniacslk-dhcp v0.0.0-20230911142651-b86573a014b1

// Patched for enabling to compile it to wasi
replace github.com/u-root/uio => github.com/ktock/u-root-uio v0.0.0-20230911142931-5cf720bc8a29
