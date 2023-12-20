var Module = {};

Module['mainScriptUrlOrBlob'] = location.origin + "/out.js";
Module['arguments'] = [
    '-m', '512M', '-nic', 'none',
    '-cpu', 'cortex-a53', '-machine', 'virt',
    '-bios', '/pack/edk2-aarch64-code.fd',
    '-kernel', '/pack/Image',
    '-append', 'earlyprintk=hvc0 console=hvc0 root=/dev/vda rootwait debug ro loglevel=7 DEBUG_VM=1 init=/sbin/tini -- /sbin/init',
    '-drive', 'if=none,id=drive1,format=raw,file=/pack/rootfs.bin',
    '-device', 'virtio-blk,drive=drive1,id=virtblk0,num-queues=1',
    '-device', 'virtio-serial,id=virtio-serial0',
    '-chardev', 'stdio,id=charconsole0',
    '-device', 'virtconsole,chardev=charconsole0,id=console0'
];

var netParam = getNetParam();
if (netParam && (netParam.mode == 'delegate')) {
    Module['arguments'] = ['--net', 'socket', '--mac', genmac()];
    Module['websocket'] = {
        'url': netParam.param
    };
}

function getNetParam() {
    var vars = location.search.substring(1).split('&');
    for (var i = 0; i < vars.length; i++) {
        var kv = vars[i].split('=');
        if (decodeURIComponent(kv[0]) == 'net') {
            return {
                mode: kv[1],
                param: kv[2],
            };
        }
    }
    return null;
}

function genmac(){
    return "02:XX:XX:XX:XX:XX".replace(/X/g, function() {
        return "0123456789ABCDEF".charAt(Math.floor(Math.random() * 16))
    });
}
