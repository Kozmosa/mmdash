# Development Sandbox image for the Local Docker Runtime.
# It intentionally contains the three fixed entrypoint runtimes supported by
# Box: Python, Node.js, and Go. The Gateway still supplies the entrypoint and
# mounts the workspace read-only and the result directory read-write.
FROM node:24-alpine AS node
FROM golang:1.26-alpine AS go
FROM python:3.12-slim

ENV DEBIAN_FRONTEND=noninteractive \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

# The development workstation already ships the Node and Go base images. Copy
# their self-contained runtimes so this image can be built without an apt or
# registry network dependency. Node's Alpine binary uses musl; its loader and
# C++ runtime are copied alongside it.
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
COPY --from=node /lib/ld-musl-x86_64.so.1 /lib/ld-musl-x86_64.so.1
COPY --from=node /usr/lib/libgcc_s.so.1 /usr/lib/libgcc_s.so.1
COPY --from=node /usr/lib/libstdc++.so.6.0.34 /usr/lib/libstdc++.so.6.0.34
RUN ln -s /usr/lib/libstdc++.so.6.0.34 /usr/lib/libstdc++.so.6
COPY --from=go /usr/local/go /usr/local/go
RUN ln -s /usr/local/go/bin/go /usr/local/bin/go \
    && ln -s /usr/local/go/bin/gofmt /usr/local/bin/gofmt

ENV GOROOT=/usr/local/go \
    PATH=/usr/local/go/bin:/usr/local/bin:$PATH

WORKDIR /workspace

# The runtime always supplies the actual command. Keep a harmless default so
# the image can be probed with `docker create`/`docker start`.
ENTRYPOINT ["/bin/true"]
