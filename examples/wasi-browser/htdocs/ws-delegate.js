function delegate(worker, workerImageName, address) {
    var shared = new SharedArrayBuffer(8 + 4096);
    var streamCtrl = new Int32Array(shared, 0, 1);
    var streamStatus = new Int32Array(shared, 4, 1);
    var streamLen = new Int32Array(shared, 8, 1);
    var streamData = new Uint8Array(shared, 12);
    worker.postMessage({type: "init", buf: shared, imagename: workerImageName});

    var opts = 'binary';
    var ongoing = false;
    var opened = false;
    var accepted = false;
    var wsconn;
    var connbuf = new Uint8Array(0);
    return function(msg) {
        const req_ = msg.data;
        if (typeof req_ == "object" && req_.type) {
            switch (req_.type) {
            case "accept":
                if (opened) {
                    streamData[0] = 1; // opened
                    accepted = true;
                } else {
                    streamData[0] = 0; // not opened
                    if (!ongoing) {
                        ongoing = true;
                        wsconn = new WebSocket(address, opts);
                        wsconn.binaryType = 'arraybuffer';
                        wsconn.onmessage = function(event) {
                            buf2 = new Uint8Array(connbuf.length + event.data.byteLength);
                            var o = connbuf.length;
                            buf2.set(connbuf, 0);
                            buf2.set(new Uint8Array(event.data), o);
                            connbuf = buf2;
                        };
                        wsconn.onclose = function(event) {
                            console.log("websocket closed" + event.code + " " + event.reason + " " + event.wasClean);
                            opened = false;
                            accepted = false;
                            ongoing = false;
                        };
                        wsconn.onopen = function(event) {
                            opened = true;
                            accepted = false;
                            ongoing = false;
                        };
                        wsconn.onerror = function(error) {
                            console.log("websocket error: "+error.data);
                            opened = false;
                            accepted = false;
                            ongoing = false;
                        };
                    }
                }
                streamStatus[0] = 0;
                break;
            case "send":
                if (!accepted) {
                    console.log("ERROR: cannot send to unaccepted websocket");
                    streamStatus[0] = -1;
                    break;
                }
                wsconn.send(req_.buf);
                streamStatus[0] = 0;
                break;
            case "recv":
                if (!accepted) {
                    console.log("ERROR: cannot receive from unaccepted websocket");
                    streamStatus[0] = -1;
                    break;
                }
                var length = req_.len;
                if (length > streamData.length)
                    length = streamData.length;
                if (length > connbuf.length)
                    length = connbuf.length
                var buf = connbuf.slice(0, length);
                var remain = connbuf.slice(length, connbuf.length);
                connbuf = remain;
                streamLen[0] = buf.length;
                streamData.set(buf, 0);
                streamStatus[0] = 0;
                break;
            case "recv-is-readable":
                if (!accepted) {
                    console.log("ERROR: cannot poll unaccepted websocket");
                    streamStatus[0] = -1;
                    break;
                }
                if (connbuf.length > 0) {
                    streamData[0] = 1; // ready for reading
                } else {
                    streamData[0] = 0; // timeout
                }
                streamStatus[0] = 0;
                break;
            default:
                console.log("unknown request: " +  req_.type)
                return;
            }
            Atomics.store(streamCtrl, 0, 1);
            Atomics.notify(streamCtrl, 0);
        }
    }
}
