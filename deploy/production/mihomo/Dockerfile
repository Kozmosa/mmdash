ARG MMDASH_CORE_IMAGE=mmdash/core:0.1.0
FROM ${MMDASH_CORE_IMAGE} AS certificates

FROM scratch

COPY --from=certificates /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --chmod=0555 mihomo /usr/local/bin/mihomo

USER 65532:65532
ENTRYPOINT ["/usr/local/bin/mihomo"]
