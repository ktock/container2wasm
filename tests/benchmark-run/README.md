Prepare pigz image

```
docker build -t pigz - < images/Dockerfile.pigz
mkdir -p /tmp/testbenchdocs/htdocs/
```

- QEMU(single)
  ```
  ./../../out/c2w --dockerfile=../../Dockerfile --assets=../../. --target-stage=js-qemu-amd64 pigz /tmp/testbenchdocs/htdocs/
  cp -R ./htdocs_qemu/* /tmp/testbenchdocs/htdocs/
  ```
- QEMU(MTTCG)
  ```
  ./../../out/c2w --dockerfile=../../Dockerfile --assets=../../. --target-stage=js-qemu-amd64 pigz /tmp/testbenchdocs/htdocs/
  cp -R ./htdocs_qemu_mttcg4/* /tmp/testbenchdocs/htdocs/
  ```
- Bochs
  ```
  ./../../out/c2w --dockerfile=../../Dockerfile --assets=../../. --to-js pigz /tmp/testbenchdocs/htdocs/
  cp -R ./htdocs_bochs/* /tmp/testbenchdocs/htdocs/
  ```

Start server

```
cp ../../examples/emscripten/xterm-pty.conf /tmp/testbenchdocs/
docker run --rm -p 8085:80 \
         -v "/tmp/testbenchdocs/htdocs:/usr/local/apache2/htdocs/:ro" \
         -v "/tmp/testbenchdocs/xterm-pty.conf:/usr/local/apache2/conf/extra/xterm-pty.conf:ro" \
         --entrypoint=/bin/sh httpd -c 'echo "Include conf/extra/xterm-pty.conf" >> /usr/local/apache2/conf/httpd.conf && httpd-foreground'

```
