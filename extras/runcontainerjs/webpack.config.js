const path = require('path');

module.exports = {
    mode: 'development',
    entry: {
        'runcontainer': './src/web/runcontainer.js',
	'stack-worker':  './src/web/stack-worker.js',
	'worker-util':  './src/web/worker-util.js',
    },
    output: {
        path: path.resolve(__dirname, 'dist'),
        filename: '[name].js',
        library: {
            name: 'RunContainer',
            type: 'var',
        },
    },
    module: {
	rules: [
	    {
		test: /\.ts$/,
		exclude: /node_modules/,
		use: [{
		    loader: 'ts-loader'
		}]
	    },
        ]
    },
    resolve: {
	mainFields: ['browser', 'module', 'main'], // look for `browser` entry point in imported node modules
	extensions: ['.ts', '.js'], // support ts-files and js-files
	alias: {
	    // provides alternate implementation for node module and source files
	}
    }
}
