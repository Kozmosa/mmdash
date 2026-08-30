ARG CADDY_BASE_IMAGE=caddy:2.10-alpine

FROM ${CADDY_BASE_IMAGE}

RUN setcap -r /usr/bin/caddy \
    && mkdir -p /tmp/caddy-config /tmp/caddy-data \
    && chown -R 65532:65532 /tmp/caddy-config /tmp/caddy-data

COPY --chown=65532:65532 --chmod=0444 deploy/production/caddy/Caddyfile /etc/caddy/Caddyfile

ENV XDG_CONFIG_HOME=/tmp/caddy-config \
    XDG_DATA_HOME=/tmp/caddy-data

USER 65532:65532

EXPOSE 8080

CMD ["caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
