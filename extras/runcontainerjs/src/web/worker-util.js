import { Event, EventType, Subscription, SubscriptionClock, SubscriptionFdReadWrite, SubscriptionU, wasiHackSocket } from './wasi-util';
import { WASI, PreopenDirectory } from "@bjorn3/browser_wasi_shim";
import * as wasitype from "@bjorn3/browser_wasi_shim";

export function startContainer(info, cargs, ttyClient) {
    var connBuf = info.net;
    registerConnBuffer(connBuf.toNet, connBuf.fromNet);
    registerMetaBuffer(connBuf.metaFromNet);
    var args = [];
    var env = [];
    var fds = [];
    var listenfd = 3;
    fetch(info.vmImage, { credentials: 'same-origin' }).then((resp) => {
        resp['blob']().then((blob) => {
            const ds = new DecompressionStream("gzip")
            new Response(blob.stream().pipeThrough(ds))['arrayBuffer']().then((wasm) => {
                recvCert().then((cert) => {
                    var certDir = getCertDir(cert);
                    fds = [
                        undefined, // 0: stdin
                        undefined, // 1: stdout
                        undefined, // 2: stderr
                        certDir,   // 3: certificates dir
                        undefined, // 4: socket listenfd
                        undefined, // 5: accepted socket fd (multi-connection is unsupported)
                        // 6...: used by wasi shim
                    ];
                    args = ['arg0', '--net=socket=listenfd=4', '--mac', genmac(), '--external-bundle=9p=192.168.127.252'];
                    args = args.concat(cargs);
                    env = [
                        "SSL_CERT_FILE=/.wasmenv/proxy.crt",
                        "https_proxy=http://192.168.127.253:80",
                        "http_proxy=http://192.168.127.253:80",
                        "HTTPS_PROXY=http://192.168.127.253:80",
                        "HTTP_PROXY=http://192.168.127.253:80"
                    ];
                    listenfd = 4;
                    startWasi(wasm, ttyClient, args, env, fds, listenfd, 5);
                });
                return;
            })
        })
    });
}

function startWasi(wasm, ttyClient, args, env, fds, listenfd, connfd) {
    var wasi = new WASI(args, env, fds);
    wasiHack(wasi, ttyClient, connfd);
    wasiHackSocket(wasi, listenfd, connfd, sockAccept, sockSend, sockRecv);
    WebAssembly.instantiate(wasm, {
        "wasi_snapshot_preview1": wasi.wasiImport,
    }).then((inst) => {
        wasi.start(inst.instance);
    });
}

// wasiHack patches wasi object for integrating it to xterm-pty.
function wasiHack(wasi, ttyClient, connfd) {
    // definition from wasi-libc https://github.com/WebAssembly/wasi-libc/blob/wasi-sdk-19/expected/wasm32-wasi/predefined-macros.txt
    const ERRNO_INVAL = 28;
    const ERRNO_AGAIN= 6;
    var _fd_read = wasi.wasiImport.fd_read;
    wasi.wasiImport.fd_read = (fd, iovs_ptr, iovs_len, nread_ptr) => {
        if (fd == 0) {
            var buffer = new DataView(wasi.inst.exports.memory.buffer);
            var buffer8 = new Uint8Array(wasi.inst.exports.memory.buffer);
            var iovecs = wasitype.wasi.Iovec.read_bytes_array(buffer, iovs_ptr, iovs_len);
            var nread = 0;
            for (var i = 0; i < iovecs.length; i++) {
                var iovec = iovecs[i];
                if (iovec.buf_len == 0) {
                    continue;
                }
                var data = ttyClient.onRead(iovec.buf_len);
                buffer8.set(data, iovec.buf);
                nread += data.length;
            }
            buffer.setUint32(nread_ptr, nread, true);
            return 0;
        } else {
            console.log("fd_read: unknown fd " + fd);
            return _fd_read.apply(wasi.wasiImport, [fd, iovs_ptr, iovs_len, nread_ptr]);
        }
        return ERRNO_INVAL;
    }
    var _fd_write = wasi.wasiImport.fd_write;
    wasi.wasiImport.fd_write = (fd, iovs_ptr, iovs_len, nwritten_ptr) => {
        if ((fd == 1) || (fd == 2)) {
            var buffer = new DataView(wasi.inst.exports.memory.buffer);
            var buffer8 = new Uint8Array(wasi.inst.exports.memory.buffer);
            var iovecs = wasitype.wasi.Ciovec.read_bytes_array(buffer, iovs_ptr, iovs_len);
            var wtotal = 0
            for (var i = 0; i < iovecs.length; i++) {
                var iovec = iovecs[i];
                var buf = buffer8.slice(iovec.buf, iovec.buf + iovec.buf_len);
                if (buf.length == 0) {
                    continue;
                }
                ttyClient.onWrite(Array.from(buf));
                wtotal += buf.length;
            }
            buffer.setUint32(nwritten_ptr, wtotal, true);
            return 0;
        } else {
            console.log("fd_write: unknown fd " + fd);
            return _fd_write.apply(wasi.wasiImport, [fd, iovs_ptr, iovs_len, nwritten_ptr]);
        }
        return ERRNO_INVAL;
    }
    wasi.wasiImport.poll_oneoff = (in_ptr, out_ptr, nsubscriptions, nevents_ptr) => {
        if (nsubscriptions == 0) {
            return ERRNO_INVAL;
        }
        let buffer = new DataView(wasi.inst.exports.memory.buffer);
        let in_ = Subscription.read_bytes_array(buffer, in_ptr, nsubscriptions);
        let isReadPollStdin = false;
        let isReadPollConn = false;
        let isClockPoll = false;
        let pollSubStdin;
        let pollSubConn;
        let clockSub;
        let timeout = Number.MAX_VALUE;
        for (let sub of in_) {
            if (sub.u.tag.variant == "fd_read") {
                if ((sub.u.data.fd != 0) && (sub.u.data.fd != connfd)) {
                    console.log("poll_oneoff: unknown fd " + sub.u.data.fd);
                    return ERRNO_INVAL; // only fd=0 and connfd is supported as of now (FIXME)
                }
                if (sub.u.data.fd == 0) {
                    isReadPollStdin = true;
                    pollSubStdin = sub;
                } else {
                    isReadPollConn = true;
                    pollSubConn = sub;
                }
            } else if (sub.u.tag.variant == "clock") {
                if (sub.u.data.timeout < timeout) {
                    timeout = sub.u.data.timeout
                    isClockPoll = true;
                    clockSub = sub;
                }
            } else {
                console.log("poll_oneoff: unknown variant " + sub.u.tag.variant);
                return ERRNO_INVAL; // FIXME
            }
        }
        if (!isClockPoll) {
            timeout = 0;
        }
        let events = [];
        if (isReadPollStdin || isReadPollConn || isClockPoll) {
            var readable = false;
            if (isReadPollStdin || (isClockPoll && timeout > 0)) {
                readable = ttyClient.onWaitForReadable(timeout / 1000000000);
            }
            if (readable && isReadPollStdin) {
                let event = new Event();
                event.userdata = pollSubStdin.userdata;
                event.error = 0;
                event.type = new EventType("fd_read");
                events.push(event);
            }
            if (isReadPollConn) {
                var sockreadable = sockWaitForReadable();
                if (sockreadable == errStatus) {
                    return ERRNO_INVAL;
                } else if (sockreadable == true) {
                    let event = new Event();
                    event.userdata = pollSubConn.userdata;
                    event.error = 0;
                    event.type = new EventType("fd_read");
                    events.push(event);
                }
            }
            if (isClockPoll) {
                let event = new Event();
                event.userdata = clockSub.userdata;
                event.error = 0;
                event.type = new EventType("clock");
                events.push(event);
            }
        }
        var len = events.length;
        Event.write_bytes_array(buffer, out_ptr, events);
        buffer.setUint32(nevents_ptr, len, true);
        return 0;
    }
}

function genmac(){
    return "02:XX:XX:XX:XX:XX".replace(/X/g, function() {
        return "0123456789ABCDEF".charAt(Math.floor(Math.random() * 16))
    });
}

var toNetCtrl;
var toNetBegin;
var toNetEnd;
var toNetNotify;
var toNetData;
var fromNetCtrl;
var fromNetBegin;
var fromNetEnd;
var fromNetData;
function registerConnBuffer(to, from) {
    toNetCtrl = new Int32Array(to, 0, 1);
    toNetBegin = new Int32Array(to, 4, 1);
    toNetEnd = new Int32Array(to, 8, 1);
    toNetNotify = new Int32Array(to, 12, 1);
    toNetData = new Uint8Array(to, 16);
    fromNetCtrl = new Int32Array(from, 0, 1);
    fromNetBegin = new Int32Array(from, 4, 1);
    fromNetEnd = new Int32Array(from, 8, 1);
    fromNetData = new Uint8Array(from, 12);
}

var metaFromNetCtrl;
var metaFromNetBegin;
var metaFromNetEnd;
var metaFromNetStatus;
var metaFromNetData;
function registerMetaBuffer(meta) {
    metaFromNetCtrl = new Int32Array(meta, 0, 1);
    metaFromNetBegin = new Int32Array(meta, 4, 1);
    metaFromNetEnd = new Int32Array(meta, 8, 1);
    metaFromNetStatus = new Int32Array(meta, 12, 1);
    metaFromNetData = new Uint8Array(meta, 16);
}

const errStatus = {
    val: 0,
};

var accepted = false;
function sockAccept(){
    accepted = true;
    return true;
}

function sockSend(data){
    if (!accepted) {
        return -1;
    }

    for(;;) {
        if (Atomics.compareExchange(toNetCtrl, 0, 0, 1) == 0) {
            break;
        }
        Atomics.wait(toNetCtrl, 0, 1);
    }
    let begin = toNetBegin[0]; //inclusive
    let end = toNetEnd[0]; //exclusive
    var len;
    var round;
    if (end >= begin) {
        len = toNetData.byteLength - end;
        round = begin;
    } else {
        len = begin - end;
        round = 0;
    }
    if ((len + round) < data.length) {
        // buffer is full; drop packets
        // TODO: preserve this
        console.log("FIXME: buffer full; dropping packets");
    } else {
        if (len > 0) {
            if (len > data.length) {
                len = data.length
            }
            toNetData.set(data.subarray(0, len), end);
            toNetEnd[0] = end + len;
        }
        if ((round > 0) && (data.length > len)) {
            if (round > data.length - len) {
                round = data.length - len
            }
            toNetData.set(data.subarray(len, len + round), 0);
            toNetEnd[0] = round;
        }
    }
    if (Atomics.compareExchange(toNetCtrl, 0, 1, 0) != 1) {
        console.log("UNEXPECTED STATUS");
    }
    Atomics.notify(toNetCtrl, 0, 1);

    Atomics.store(toNetNotify, 0, 1);
    Atomics.notify(toNetNotify, 0);
    return 0;
}

function sockRecv(targetBuf, targetOffset, targetLen){
    if (!accepted) {
        return -1;
    }

    for(;;) {
        if (Atomics.compareExchange(fromNetCtrl, 0, 0, 1) == 0) {
            break;
        }
        Atomics.wait(fromNetCtrl, 0, 1);
    }
    let begin = fromNetBegin[0]; //inclusive
    let end = fromNetEnd[0]; //exclusive
    var len;
    var round;
    if (end >= begin) {
        len = end - begin;
        round = 0;
    } else {
        len = fromNetData.byteLength - begin;
        round = end;
    }
    if (targetLen < len) {
        len = targetLen;
        round = 0;
    } else if (targetLen < len + round) {
        round = targetLen - len;
    }
    if (len > 0) {
        targetBuf.set(fromNetData.subarray(begin, begin + len), targetOffset);
        fromNetBegin[0] = begin + len;
    }
    if (round > 0) {
        targetBuf.set(fromNetData.subarray(0, round), targetOffset + len);
        fromNetBegin[0] = round;
    }
    if (Atomics.compareExchange(fromNetCtrl, 0, 1, 0) != 1) {
        console.log("UNEXPECTED STATUS");
    }
    Atomics.notify(fromNetCtrl, 0, 1);

    return (len + round);
}

function sockWaitForReadable(){
    if (!accepted) {
        errStatus.val = -1;
        return errStatus;
    }

    for(;;) {
        if (Atomics.compareExchange(fromNetCtrl, 0, 0, 1) == 0) {
            break;
        }
        Atomics.wait(fromNetCtrl, 0, 1);
    }
    let begin = fromNetBegin[0]; //inclusive
    let end = fromNetEnd[0]; //exclusive
    var len;
    var round;
    if (end >= begin) {
        len = end - begin;
        round = 0;
    } else {
        len = fromNetData.byteLength - begin;
        round = end;
    }
    var ready;
    if ((len + round) > 0) {
        ready = true;
    } else {
        ready = false;
    }
    if (Atomics.compareExchange(fromNetCtrl, 0, 1, 0) != 1) {
        console.log("UNEXPECTED STATUS");
    }
    Atomics.notify(fromNetCtrl, 0, 1);

    return ready;
}

function recvCert(){
    var buf = new Uint8Array(0);
    return new Promise((resolve, reject) => {
        function getCert(){
            var done = false;
            for(;;) {
                if (Atomics.compareExchange(metaFromNetCtrl, 0, 0, 1) == 0) {
                    break;
                }
                Atomics.wait(metaFromNetCtrl, 0, 1);
            }
            let end = metaFromNetEnd[0]; //exclusive
            if (end > 0) {
                buf = appendData(buf, metaFromNetData.slice(0, end));
                metaFromNetEnd[0] = 0;
            }
            if (metaFromNetStatus[0] == 1) {
                done = true;
            }
            if (Atomics.compareExchange(metaFromNetCtrl, 0, 1, 0) != 1) {
                console.log("UNEXPECTED STATUS");
            }
            Atomics.notify(metaFromNetCtrl, 0, 1);

            if (done) {
                resolve(buf); // EOF
            } else {
                setTimeout(getCert, 0);
                return;
            }
        }
        getCert();
    });
}

function appendData(data1, data2) {
    var buf2 = new Uint8Array(data1.byteLength + data2.byteLength);
    buf2.set(new Uint8Array(data1), 0);
    buf2.set(new Uint8Array(data2), data1.byteLength);
    return buf2;
}

function getCertDir(cert) {
    var certDir = new PreopenDirectory("/.wasmenv", {
        "proxy.crt": new File(cert, {})
    });
    var _path_open = certDir.path_open;
    certDir.path_open = (e, r, s, n, a, d) => {
        var ret = _path_open.apply(certDir, [e, r, s, n, a, d]);
        if (ret.fd_obj != null) {
            var o = ret.fd_obj;
            ret.fd_obj.fd_pread = (view8, iovs, offset) => {
                var old_offset = o.file_pos;
                var r = o.fd_seek(offset, WHENCE_SET);
                if (r.ret != 0) {
                    return { ret: -1, nread: 0 };
                }
                var read_ret = o.fd_read(view8, iovs);
                r = o.fd_seek(old_offset, WHENCE_SET);
                if (r.ret != 0) {
                    return { ret: -1, nread: 0 };
                }
                return read_ret;
            }
        }
        return ret;
    }
    certDir.dir.contents["."] = certDir.dir;
    return certDir;
}
