FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH
WORKDIR /src

COPY go.mod go.sum /src/
COPY relay /src/relay
COPY headless /src/headless

RUN go -C /src/headless/vk        build -trimpath -ldflags="-s -w" -o /out/headless-vk-creator . \
 && go -C /src/headless/telemost  build -trimpath -ldflags="-s -w" -o /out/headless-telemost-creator . \
 && go -C /src/headless/wbstream  build -trimpath -ldflags="-s -w" -o /out/headless-wbstream-creator . \
 && go -C /src/headless/dion      build -trimpath -ldflags="-s -w" -o /out/headless-dion-creator . \
 && go -C /src/headless/vk-bot    build -trimpath -ldflags="-s -w" -o /out/headless-vk-bot .

FROM debian:bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates tini \
 && rm -rf /var/lib/apt/lists/*

RUN useradd --system --create-home --home /home/wlb --shell /usr/sbin/nologin wlb \
 && mkdir -p /opt/wlb/bin /data/sessions \
 && chown -R wlb:wlb /data

COPY --from=build /out/headless-vk-creator        /opt/wlb/bin/
COPY --from=build /out/headless-telemost-creator  /opt/wlb/bin/
COPY --from=build /out/headless-wbstream-creator  /opt/wlb/bin/
COPY --from=build /out/headless-dion-creator      /opt/wlb/bin/
COPY --from=build /out/headless-vk-bot            /usr/local/bin/

COPY headless/docker/entrypoint.sh /usr/local/bin/wlb-entrypoint
RUN chmod +x /usr/local/bin/wlb-entrypoint

USER wlb
WORKDIR /data
VOLUME ["/data"]

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/wlb-entrypoint"]
