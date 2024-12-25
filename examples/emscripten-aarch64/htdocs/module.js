Module['mainScriptUrlOrBlob'] = location.origin + "/out.js";
Module['preRun'].push((mod) => {
    mod.FS.mkdir('/pack');
    mod.FS.writeFile('/pack/info', ''); // TODO: write info config
});
