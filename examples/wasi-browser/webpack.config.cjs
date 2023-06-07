const path = require("path")

module.exports = {
    entry: {
        "index": "./dist/index.js",
        "wasi_defs": "./dist/wasi_defs.js"
    },
    output: {
        path: path.resolve(__dirname,'out'),
        filename: "[name].js",
        libraryTarget: 'umd',
    },
};
