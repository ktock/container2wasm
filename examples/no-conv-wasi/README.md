# No Conversion with WASI
Using `imagemounter`, which works great for browsers, comes with some unnecessary overhead. This is an alternative solution that bypasses the network and uses the built-in WASI capability to map folders.  
The caveat is that the WASI filesystem does not have permissions. To overcome this, we export the container rootfs as a SquashFS image.

# Create the Wasm Module
Here I'm targeting RISC-V:
```console
c2w --target-arch=riscv64 --external-bundle riscv64-squash.wasm
```

# Convert the Container
```console
d2oci --target-arch riscv64 riscv64/python:3.8-alpine /tmp/riscv-python
```

The generated images are way more compact:
```console 
$ du -sh /tmp/riscv-python/blobs/
46M     /tmp/riscv-python/blobs/

$ du -sh /tmp/riscv-python/rootfs
50M     /tmp/riscv-python/rootfs

$ du -sh /tmp/riscv-python/rootfs.bin 
17M     /tmp/riscv-python/rootfs.bin
```

# Run the Container
```console
wazero run --mount /tmp/riscv-python:/ext/bundle riscv64-squash.wasm python
```

Or,
```console
wazero run --mount /tmp/riscv-python:/some/folder riscv64-squash.wasm --external-bundle /some/folder python
```