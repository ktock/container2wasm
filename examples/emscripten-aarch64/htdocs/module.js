Module['mainScriptUrlOrBlob'] = location.origin + "/out.js";
Module['arguments'] = [
    '-cpu', 'cortex-a53', '-machine', 'virt',
    '-bios', '/pack/edk2-aarch64-code.fd',
    '-m', '512M', '-accel', 'tcg,tb-size=500',
    //Use the following to enable MTTCG
    //'-m', '512M', '-accel', 'tcg,tb-size=500,thread=multi', '-smp', '4,sockets=4',
    '-nic', 'none',
    '-drive', 'if=virtio,format=raw,file=/pack/rootfs.bin',
    '-kernel', '/pack/bzImage',
    '-append', 'earlyprintk=hvc0 console=hvc0 root=/dev/vda rootwait ro loglevel=7 NO_RUNTIME_CONFIG=1 init=/sbin/tini -- /sbin/init',
    '-device', 'virtio-serial,id=virtio-serial0',
    '-chardev', 'stdio,id=charconsole0,mux=on',
    '-device', 'virtconsole,chardev=charconsole0,id=console0'
];
