# Patches to hook LibreChat's Run Code button in the browser

This is a set of patches to hook LibreChat's `execute_code` API and serve it inside browser.
The code block is executed in the c2w container inside browser.
For now, the code block is just sent to the shell stdin of an Alpine container so only the shell code block works.

`build.sh` generates the c2w-related assets. Put them to `/client/public/librechat-c2w-patch/` in the LibreChat repo.

LibreChat itself needs at least the following patches to inject this hook.

```
diff --git a/client/src/main.jsx b/client/src/main.jsx
index 491f13f63..bda53b9d9 100644
--- a/client/src/main.jsx
+++ b/client/src/main.jsx
@@ -12,6 +12,22 @@ import 'katex/dist/contrib/copy-tex.js';
 const container = document.getElementById('root');
 const root = createRoot(container);
 
+import axios from 'axios';
+
+let script = document.createElement('script');
+script.src = `/librechat-c2w-patch/src.js`;
+script.onload = () => console.log('loaded', script.src);
+document.head.appendChild(script);
+
+script = document.createElement('script');
+script.src = `/librechat-c2w-patch/runcontainer.js`;
+script.onload = () => console.log('loaded', script.src);
+document.head.appendChild(script);
+
+script.onload = () => {
+  window.C2WPatch.install(axios);
+};
+
 root.render(
   <ApiErrorBoundaryProvider>
     <App />
```

The following enables the Run Code button on the shell code blocks, which is convenient for testing.

```
diff --git a/client/src/utils/languages.ts b/client/src/utils/languages.ts
index 2a7d83e89..192f13b2f 100644
--- a/client/src/utils/languages.ts
+++ b/client/src/utils/languages.ts
@@ -365,6 +365,7 @@ enum Languages {
   py = 'py',
   rs = 'rs',
   ts = 'ts',
+  bash = 'bash',
 }
 
 // Create a mapping of common variations to the enum values
@@ -413,6 +414,10 @@ const languageAliases: Record<string, Languages | undefined> = {
   // TypeScript
   ts: Languages.ts,
   typescript: Languages.ts,
+
+  bash: Languages.bash,
+  shell: Languages.bash,
+  sh: Languages.bash,
 };
 
 export function normalizeLanguage(lang: string): Languages | string {
```

You might want some other changes for the build config.
At least the COI headers should be necessary for using SharedArrayBuffer.

```
diff --git a/client/vite.config.ts b/client/vite.config.ts
index 11c2cc1b0..45116cd95 100644
--- a/client/vite.config.ts
+++ b/client/vite.config.ts
@@ -53,6 +53,10 @@ export default defineConfig(({ command }) => ({
         changeOrigin: true,
       },
     },
+    headers: {
+      'Cross-Origin-Opener-Policy': 'same-origin',
+      'Cross-Origin-Embedder-Policy': 'require-corp',
+    },
   },
   // Set the directory where environment variables are loaded from and restrict prefixes
   envDir: '../',
```
