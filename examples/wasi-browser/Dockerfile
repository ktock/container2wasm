ARG BROWSER_WASI_SHIM_VERSION=v0.2.9
ARG NODE_VERSION=20.2.0

FROM node:${NODE_VERSION} AS dev
ARG BROWSER_WASI_SHIM_VERSION

RUN mkdir /work
WORKDIR /work
RUN git clone https://github.com/bjorn3/browser_wasi_shim
WORKDIR /work/browser_wasi_shim
RUN git checkout ${BROWSER_WASI_SHIM_VERSION}
RUN npm install
RUN npm install webpack webpack-cli
COPY ./webpack.config.cjs .
RUN npx webpack --mode production

FROM scratch
COPY --link --from=dev /work/browser_wasi_shim/out/* /browser_wasi_shim/
