# Running containers on browser with QEMU with networking enabled

c2w experimentally integrates QEMU TCG on Wasm(emscripten), using [QEMU Wasm](https://github.com/ktock/qemu-wasm).
Multicore CPUs can also be enabled leveraging MTTCG(Multi-Threded TCG).

[Networking](../networking) is also enabled.

## Step 1: Converting an image to Wasm

### Converting x86_64 container to Wasm

```
$ mkdir -p /tmp/out-js4/htdocs
$ c2w --dockerfile=Dockerfile --assets=. --target-stage=js-qemu-amd64 alpine /tmp/out-js4/htdocs/
```

### Converting AArch64 container to Wasm

```
$ mkdir -p /tmp/out-js4/htdocs
$ c2w --dockerfile=Dockerfile --assets=. --to-js --target-arch=aarch64 arm64v8/alpine:3.20 /tmp/out-js4/htdocs/
```

### Converting RISC-V container to Wasm

```
$ mkdir -p /tmp/out-js4/htdocs
$ c2w --dockerfile=Dockerfile --assets=. --target-arch=riscv64 --target-stage=js-qemu-riscv64 riscv64/alpine /tmp/out-js4/htdocs/
```

## Step 2: Start a server

Serve the image as the following (run them at the repository root dir):

```
$ ( cd ./examples/emscripten-qemu/htdocs/ && npx webpack && cp -R index.html dist vendor/xterm.css /tmp/out-js4/htdocs/ )
$ wget -O /tmp/c2w-net-proxy.wasm https://github.com/ktock/container2wasm/releases/download/v0.5.0/c2w-net-proxy.wasm
$ cat /tmp/c2w-net-proxy.wasm | gzip > /tmp/out-js4/htdocs/c2w-net-proxy.wasm.gzip
$ cp ./examples/emscripten-qemu/xterm-pty.conf /tmp/out-js4/
$ docker run --rm -p 8082:80 \
         -v "/tmp/out-js4/htdocs:/usr/local/apache2/htdocs/:ro" \
         -v "/tmp/out-js4/xterm-pty.conf:/usr/local/apache2/conf/extra/xterm-pty.conf:ro" \
         --entrypoint=/bin/sh httpd -c 'echo "Include conf/extra/xterm-pty.conf" >> /usr/local/apache2/conf/httpd.conf && httpd-foreground'
```

> Alternatively, you can also build c2w-net-proxy.wasm as the following:
>
> NOTE: Run this at the project repo root directory. Go >= 1.21 is needed on your machine.
> 
> ```
> $ make c2w-net-proxy.wasm
> ```

## Step 3: Accessing to the pages

The server started by the above steps provides the following pages.

### `localhost:8082?net=browser` 

This URL serves the image with enabling [networking leveraging Fetch API](../networking/fetch/).
For example, you can accesses to a site using wget (`wget -O - https://ktock.github.io/container2wasm-demo/`).

This is implemented by running a networking stack ([`c2w-net-proxy`](../../../extras/c2w-net-proxy)) inside browser and connecting it to emscripten levaraging [Mock service Worker's WebSocket interception](https://mswjs.io/docs/basics/handling-websocket-events/)

Please refer to [`../networking/fetch/`](../networking/fetch/) for more details about Fetch-API-based networking.

### `localhost:8082?net=delegate=ws://localhost:9999`

This serves the image with enabling [networking leveraging WebSocket API](../networking/websocket/).
On the host, you need to start the network stack listening on the WebSocket (e.g. `localhost:9999`) in advance.

```
$ c2w-net --listen-ws localhost:9999
```

> NOTE1: `localhost:9999` can be changed to another address.
> NOTE2: `c2w-net` is available on the [release page](https://github.com/ktock/container2wasm/releases).

Please refer to [`../networking/websocket/`](../networking/websocket/) for more details about WebSocket-based networking.

### `localhost:8082`

This starts a container without NW.
