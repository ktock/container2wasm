var Module = {};

var netParam = getNetParam();
if (netParam && (netParam.mode == 'delegate')) {
    Module['arguments'] = ['--net', 'qemu', '--mac', genmac()];
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
